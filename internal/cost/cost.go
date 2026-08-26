package cost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Source says where an entry's figure came from. A summary that mixes measured
// and inferred numbers without distinguishing them reads as precision it does
// not have, so every entry records which it is.
const (
	// SourceMeasured means the runtime reported token counts for the turn.
	SourceMeasured = "measured"
	// SourceElapsed means nothing reported tokens, so the figure was inferred
	// from how long the worker ran. It is a proxy, not a price.
	SourceElapsed = "elapsed"
)

type Entry struct {
	TaskID    string        `json:"task_id"`
	Runtime   string        `json:"runtime"`
	Model     string        `json:"model"`
	Duration  time.Duration `json:"duration_ns"`
	Status    string        `json:"status"`
	Estimate  float64       `json:"estimate_usd"`
	Timestamp time.Time     `json:"ts"`

	// Usage is what the runtime reported, when it reported anything.
	Usage *Usage `json:"usage,omitempty"`
	// Source is SourceMeasured or SourceElapsed.
	Source string `json:"source,omitempty"`
}

// Usage is a turn's token accounting as the runtime reported it.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CachedReadTokens int `json:"cached_read_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type Summary struct {
	TotalTasks    int                `json:"total_tasks"`
	TotalEstimate float64            `json:"total_estimate_usd"`
	ByModel       map[string]float64 `json:"by_model"`
	ByRuntime     map[string]float64 `json:"by_runtime"`
	ByStatus      map[string]int     `json:"by_status"`

	// MeasuredTasks counts entries priced from reported tokens, and
	// MeasuredEstimate is their share of the total. The gap between
	// MeasuredEstimate and TotalEstimate is how much of the figure is inferred
	// from elapsed time rather than counted.
	MeasuredTasks    int     `json:"measured_tasks"`
	MeasuredEstimate float64 `json:"measured_estimate_usd"`

	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CachedTokens int `json:"cached_read_tokens"`
}

// CacheRate is the share of all input served from a prompt cache, or -1 when
// nothing measured reported any input at all.
//
// Input and cached reads are disjoint counters, so the denominator is their sum.
//
// This is the figure that says whether re-priming a worker each phase is
// actually costing anything: a high rate means the repeated prefix is being
// served cheaply, and the case for carrying one session across phases is
// correspondingly weaker.
func (s *Summary) CacheRate() float64 {
	total := s.InputTokens + s.CachedTokens
	if total <= 0 {
		return -1
	}
	return float64(s.CachedTokens) / float64(total)
}

// CacheRate is the share of one entry's input served from cache, or -1 when the
// entry reported no input.
func (e Entry) CacheRate() float64 {
	if e.Usage == nil {
		return -1
	}
	total := e.Usage.InputTokens + e.Usage.CachedReadTokens
	if total <= 0 {
		return -1
	}
	return float64(e.Usage.CachedReadTokens) / float64(total)
}

var modelRates = map[string]float64{
	"opus":          0.075,
	"sonnet":        0.015,
	"kimi":          0.002,
	"deepseek":      0.001,
	"deepseek-r1":   0.003,
	"gemini-flash":  0.001,
	"gpt-4o":        0.025,
	"claude-sonnet": 0.015,
	"gemini":        0.005,
	"grok":          0.010,
	"test":          0.000,
}

// TokenRate prices a model per million tokens. Rates are coarse defaults meant
// to make relative cost between models visible, not to reproduce a bill; set
// cost_rates in config to override any of them.
type TokenRate struct {
	InputPerM  float64 `yaml:"input_per_million" json:"input_per_million"`
	OutputPerM float64 `yaml:"output_per_million" json:"output_per_million"`
	// CachedPerM prices tokens served from a prompt cache. Zero means cached
	// reads are billed at the input rate.
	CachedPerM float64 `yaml:"cached_per_million" json:"cached_per_million"`
}

var tokenRates = map[string]TokenRate{
	"opus":          {InputPerM: 15, OutputPerM: 75, CachedPerM: 1.5},
	"sonnet":        {InputPerM: 3, OutputPerM: 15, CachedPerM: 0.3},
	"claude-sonnet": {InputPerM: 3, OutputPerM: 15, CachedPerM: 0.3},
	"haiku":         {InputPerM: 0.8, OutputPerM: 4, CachedPerM: 0.08},
	"gpt-4o":        {InputPerM: 2.5, OutputPerM: 10},
	"gemini":        {InputPerM: 1.25, OutputPerM: 5},
	"gemini-flash":  {InputPerM: 0.075, OutputPerM: 0.3},
	"kimi":          {InputPerM: 0.6, OutputPerM: 2.5},
	"deepseek":      {InputPerM: 0.3, OutputPerM: 1.2},
	"deepseek-r1":   {InputPerM: 0.55, OutputPerM: 2.2},
	"grok":          {InputPerM: 2, OutputPerM: 10},
	"test":          {},
}

// defaultTokenRate prices a model nothing knows about. Charging something keeps
// an unknown model from looking free next to a priced one.
//
// The cached rate is a tenth of input, which is roughly where every provider in
// the table above puts it. Leaving it at zero would bill cached reads as fresh
// input, and a measured pipeline run came back 98% cached — so the fallback for
// an unrecognised model would have overstated it by an order of magnitude.
var defaultTokenRate = TokenRate{InputPerM: 1, OutputPerM: 4, CachedPerM: 0.1}

// SetTokenRates merges configured overrides over the built-in defaults.
func SetTokenRates(overrides map[string]TokenRate) {
	for model, rate := range overrides {
		tokenRates[model] = rate
	}
}

// RateFor returns the rate used for a model, and whether it was a known one.
func RateFor(model string) (TokenRate, bool) {
	rate, ok := tokenRates[model]
	if !ok {
		return defaultTokenRate, false
	}
	return rate, true
}

// EstimateFromTokens prices a turn from what the runtime counted.
//
// InputTokens and CachedReadTokens are disjoint: input counts what was sent
// fresh and cached counts what was served from a prompt cache, so total input is
// their sum. Measured against OpenCode, total == input + output + cached exactly,
// and the convention matches how the Anthropic API reports the same figures.
//
// This started out assuming input included cached, which under-billed and made
// cache rates read above 100%.
func EstimateFromTokens(model string, u Usage) float64 {
	rate, _ := RateFor(model)

	cachedRate := rate.CachedPerM
	if cachedRate <= 0 {
		// No cache rate configured: cached reads bill as ordinary input rather
		// than free.
		cachedRate = rate.InputPerM
	}

	return float64(u.InputTokens)/1e6*rate.InputPerM +
		float64(u.CachedReadTokens)/1e6*cachedRate +
		float64(u.OutputTokens)/1e6*rate.OutputPerM
}

// EstimateCost infers a figure from how long a worker ran. It is the fallback
// for runtimes that report no tokens.
func EstimateCost(model string, duration time.Duration) float64 {
	rate, ok := modelRates[model]
	if !ok {
		rate = 0.010
	}
	minutes := duration.Minutes()
	if minutes < 1 {
		minutes = 1
	}
	return rate * minutes
}

type Tracker struct {
	path string
}

func NewTracker(dir string) (*Tracker, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating cost directory: %w", err)
	}
	return &Tracker{path: filepath.Join(dir, "costs.ndjson")}, nil
}

func (t *Tracker) Record(entry Entry) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	// Prefer what the runtime counted. Elapsed time is only a stand-in for
	// runtimes that report nothing.
	if entry.Source == "" {
		if entry.Usage != nil && (entry.Usage.InputTokens > 0 || entry.Usage.OutputTokens > 0 ||
			entry.Usage.CachedReadTokens > 0) {
			entry.Source = SourceMeasured
		} else {
			entry.Source = SourceElapsed
		}
	}
	if entry.Estimate == 0 {
		if entry.Source == SourceMeasured {
			entry.Estimate = EstimateFromTokens(entry.Model, *entry.Usage)
		} else {
			entry.Estimate = EstimateCost(entry.Model, entry.Duration)
		}
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling cost entry: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(t.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening cost log: %w", err)
	}
	defer f.Close() //nolint:errcheck

	_, err = f.Write(line)
	return err
}

func (t *Tracker) Summarize() (*Summary, error) {
	entries, err := t.ReadAll()
	if err != nil {
		return nil, err
	}

	s := &Summary{
		ByModel:   make(map[string]float64),
		ByRuntime: make(map[string]float64),
		ByStatus:  make(map[string]int),
	}

	for _, e := range entries {
		s.TotalTasks++
		s.TotalEstimate += e.Estimate
		s.ByModel[e.Model] += e.Estimate
		s.ByRuntime[e.Runtime] += e.Estimate
		s.ByStatus[e.Status]++

		if e.Source == SourceMeasured && e.Usage != nil {
			s.MeasuredTasks++
			s.MeasuredEstimate += e.Estimate
			s.InputTokens += e.Usage.InputTokens
			s.OutputTokens += e.Usage.OutputTokens
			s.CachedTokens += e.Usage.CachedReadTokens
		}
	}

	return s, nil
}

func (t *Tracker) ReadAll() ([]Entry, error) {
	data, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading cost log: %w", err)
	}

	var entries []Entry
	for _, line := range splitLines(string(data)) {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

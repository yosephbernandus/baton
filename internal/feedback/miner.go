package feedback

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Miner struct {
	EventLogPath   string
	Window         time.Duration
	MinOccurrences int
}

func NewMiner(eventLogPath string, window time.Duration, minOccurrences int) *Miner {
	if window <= 0 {
		window = 7 * 24 * time.Hour
	}
	if minOccurrences <= 0 {
		minOccurrences = 3
	}
	return &Miner{
		EventLogPath:   eventLogPath,
		Window:         window,
		MinOccurrences: minOccurrences,
	}
}

type Analysis struct {
	GeneratedAt        time.Time                         `yaml:"generated_at" json:"generated_at"`
	EventWindow        EventWindow                       `yaml:"event_window" json:"event_window"`
	RuntimePerformance map[string]*RuntimeMetrics        `yaml:"runtime_performance" json:"runtime_performance"`
	PhaseMetrics       map[string]*PhaseMetric           `yaml:"phase_metrics" json:"phase_metrics"`
	ComplexityOutcomes map[string]*ComplexityOutcome      `yaml:"complexity_outcomes,omitempty" json:"complexity_outcomes,omitempty"`
	Patterns           []Pattern                         `yaml:"patterns" json:"patterns"`
}

type ComplexityOutcome struct {
	Tasks    int     `yaml:"tasks" json:"tasks"`
	L2Cycles int     `yaml:"l2_cycles" json:"l2_cycles"`
	L3Cycles int     `yaml:"l3_cycles" json:"l3_cycles"`
	L2Rate   float64 `yaml:"l2_rate" json:"l2_rate"`
}

type EventWindow struct {
	From        time.Time `yaml:"from" json:"from"`
	To          time.Time `yaml:"to" json:"to"`
	TotalEvents int       `yaml:"total_events" json:"total_events"`
	TotalTasks  int       `yaml:"total_tasks" json:"total_tasks"`
}

type RuntimeMetrics struct {
	Tasks        int                       `yaml:"tasks" json:"tasks"`
	Successes    int                       `yaml:"successes" json:"successes"`
	Failures     int                       `yaml:"failures" json:"failures"`
	SuccessRate  float64                   `yaml:"success_rate" json:"success_rate"`
	AvgRetries   float64                   `yaml:"avg_l1_retries" json:"avg_l1_retries"`
	TotalRetries int                       `yaml:"-" json:"-"`
	ByDomain     map[string]*DomainMetrics `yaml:"by_domain,omitempty" json:"by_domain,omitempty"`
}

type DomainMetrics struct {
	Tasks       int     `yaml:"tasks" json:"tasks"`
	Successes   int     `yaml:"successes" json:"successes"`
	SuccessRate float64 `yaml:"success_rate" json:"success_rate"`
}

type PhaseMetric struct {
	TotalRuns      int                               `yaml:"total_runs" json:"total_runs"`
	TotalRetries   int                               `yaml:"total_retries" json:"total_retries"`
	AvgRetries     float64                           `yaml:"avg_retries" json:"avg_retries"`
	LoopDetections int                               `yaml:"loop_detections" json:"loop_detections"`
	Timeouts       int                               `yaml:"timeouts" json:"timeouts"`
	ByComplexity   map[string]*ComplexityPhaseMetric `yaml:"by_complexity,omitempty" json:"by_complexity,omitempty"`
}

type ComplexityPhaseMetric struct {
	Runs       int     `yaml:"runs" json:"runs"`
	Retries    int     `yaml:"retries" json:"retries"`
	AvgRetries float64 `yaml:"avg_retries" json:"avg_retries"`
	Exhausted  int     `yaml:"exhausted" json:"exhausted"`
}

type Pattern struct {
	Type        string   `yaml:"type" json:"type"`
	Description string   `yaml:"description" json:"description"`
	Confidence  string   `yaml:"confidence" json:"confidence"`
	Occurrences int      `yaml:"occurrences" json:"occurrences"`
	Suggestion  string   `yaml:"suggestion" json:"suggestion"`
	Evidence    []string `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

type rawEvent struct {
	Timestamp time.Time              `json:"ts"`
	TaskID    string                 `json:"task_id"`
	Runtime   string                 `json:"runtime"`
	Model     string                 `json:"model"`
	EventType string                 `json:"event"`
	Data      map[string]interface{} `json:"data"`
}

func (m *Miner) Analyze() (*Analysis, error) {
	cutoff := time.Now().Add(-m.Window)
	events, err := m.readEvents(cutoff)
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return &Analysis{
			GeneratedAt:        time.Now().UTC(),
			RuntimePerformance: make(map[string]*RuntimeMetrics),
			PhaseMetrics:       make(map[string]*PhaseMetric),
			ComplexityOutcomes: make(map[string]*ComplexityOutcome),
		}, nil
	}

	analysis := &Analysis{
		GeneratedAt:        time.Now().UTC(),
		RuntimePerformance: make(map[string]*RuntimeMetrics),
		PhaseMetrics:       make(map[string]*PhaseMetric),
		ComplexityOutcomes: make(map[string]*ComplexityOutcome),
	}

	taskSet := make(map[string]bool)
	var earliest, latest time.Time

	for _, ev := range events {
		taskSet[ev.TaskID] = true
		if earliest.IsZero() || ev.Timestamp.Before(earliest) {
			earliest = ev.Timestamp
		}
		if latest.IsZero() || ev.Timestamp.After(latest) {
			latest = ev.Timestamp
		}

		runtimeKey := ev.Runtime + "/" + ev.Model
		if ev.Runtime == "" && ev.Model == "" {
			runtimeKey = ""
		}

		switch ev.EventType {
		case "task_completed", "task_failed":
			if runtimeKey == "" {
				continue
			}
			rm := m.ensureRuntime(analysis, runtimeKey)
			rm.Tasks++
			if ev.EventType == "task_completed" {
				rm.Successes++
			} else {
				rm.Failures++
			}
			if domain := m.extractDomain(ev); domain != "" {
				dm := m.ensureDomain(rm, domain)
				dm.Tasks++
				if ev.EventType == "task_completed" {
					dm.Successes++
				}
			}

		case "phase_started":
			phaseName := m.extractString(ev, "phase_name")
			if phaseName == "" {
				continue
			}
			pm := m.ensurePhase(analysis, phaseName)
			pm.TotalRuns++
			complexity := m.extractString(ev, "complexity")
			if complexity != "" {
				cpm := m.ensureComplexityPhase(pm, complexity)
				cpm.Runs++
				if phaseName == "setup" {
					co := m.ensureComplexityOutcome(analysis, complexity)
					co.Tasks++
				}
			}

		case "phase_retry":
			phaseName := m.extractString(ev, "phase_name")
			if phaseName == "" {
				continue
			}
			pm := m.ensurePhase(analysis, phaseName)
			pm.TotalRetries++
			if runtimeKey != "" {
				rm := m.ensureRuntime(analysis, runtimeKey)
				rm.TotalRetries++
			}
			complexity := m.extractString(ev, "complexity")
			if complexity != "" {
				cpm := m.ensureComplexityPhase(pm, complexity)
				cpm.Retries++
			}

		case "phase_stuck":
			phaseName := m.extractString(ev, "phase_name")
			if phaseName == "" {
				continue
			}
			pm := m.ensurePhase(analysis, phaseName)
			pm.LoopDetections++

		case "phase_failed":
			phaseName := m.extractString(ev, "phase_name")
			complexity := m.extractString(ev, "complexity")
			if phaseName != "" && complexity != "" {
				pm := m.ensurePhase(analysis, phaseName)
				cpm := m.ensureComplexityPhase(pm, complexity)
				cpm.Exhausted++
			}

		case "worker_timeout":
			phaseName := m.extractString(ev, "phase_name")
			if phaseName != "" {
				pm := m.ensurePhase(analysis, phaseName)
				pm.Timeouts++
			}

		case "l2_loop_back":
			complexity := m.extractString(ev, "complexity")
			if complexity != "" {
				co := m.ensureComplexityOutcome(analysis, complexity)
				co.L2Cycles++
			}

		case "l3_fresh_approach":
			complexity := m.extractString(ev, "complexity")
			if complexity != "" {
				co := m.ensureComplexityOutcome(analysis, complexity)
				co.L3Cycles++
			}
		}
	}

	analysis.EventWindow = EventWindow{
		From:        earliest,
		To:          latest,
		TotalEvents: len(events),
		TotalTasks:  len(taskSet),
	}

	for _, rm := range analysis.RuntimePerformance {
		if rm.Tasks > 0 {
			rm.SuccessRate = float64(rm.Successes) / float64(rm.Tasks)
			rm.AvgRetries = float64(rm.TotalRetries) / float64(rm.Tasks)
		}
		for _, dm := range rm.ByDomain {
			if dm.Tasks > 0 {
				dm.SuccessRate = float64(dm.Successes) / float64(dm.Tasks)
			}
		}
	}
	for _, pm := range analysis.PhaseMetrics {
		if pm.TotalRuns > 0 {
			pm.AvgRetries = float64(pm.TotalRetries) / float64(pm.TotalRuns)
		}
		for _, cpm := range pm.ByComplexity {
			if cpm.Runs > 0 {
				cpm.AvgRetries = float64(cpm.Retries) / float64(cpm.Runs)
			}
		}
	}

	analysis.Patterns = m.detectPatterns(analysis)
	return analysis, nil
}

func (m *Miner) detectPatterns(a *Analysis) []Pattern {
	var patterns []Pattern

	// runtime_domain_mismatch: success rate <60% for a domain with enough tasks
	for rtKey, rm := range a.RuntimePerformance {
		for domain, dm := range rm.ByDomain {
			if dm.Tasks >= m.MinOccurrences && dm.SuccessRate < 0.6 {
				conf := "medium"
				if dm.Tasks >= m.MinOccurrences*2 {
					conf = "high"
				}
				patterns = append(patterns, Pattern{
					Type:        "runtime_domain_mismatch",
					Description: fmt.Sprintf("%s fails %.0f%% of %s tasks", rtKey, (1-dm.SuccessRate)*100, domain),
					Confidence:  conf,
					Occurrences: dm.Tasks - dm.Successes,
					Suggestion:  fmt.Sprintf("Route %s tasks to a different runtime", domain),
				})
			}
		}
	}

	// retry_budget_insufficient: >30% of tasks at a complexity exhaust L1
	for phaseName, pm := range a.PhaseMetrics {
		for complexity, cpm := range pm.ByComplexity {
			if cpm.Runs >= m.MinOccurrences && cpm.Exhausted > 0 {
				exhaustRate := float64(cpm.Exhausted) / float64(cpm.Runs)
				if exhaustRate > 0.3 {
					patterns = append(patterns, Pattern{
						Type:        "retry_budget_insufficient",
						Description: fmt.Sprintf("%s tasks exhaust L1 budget in %s phase %.0f%% of the time", complexity, phaseName, exhaustRate*100),
						Confidence:  "medium",
						Occurrences: cpm.Exhausted,
						Suggestion:  fmt.Sprintf("Increase max_l1_retries for %s complexity", complexity),
					})
				}
			}
		}
	}

	// retry_budget_excessive: avg retries <0.5 with enough data
	for phaseName, pm := range a.PhaseMetrics {
		for complexity, cpm := range pm.ByComplexity {
			if cpm.Runs >= m.MinOccurrences && cpm.AvgRetries < 0.5 && cpm.Retries == 0 {
				patterns = append(patterns, Pattern{
					Type:        "retry_budget_excessive",
					Description: fmt.Sprintf("%s tasks never need retries in %s phase", complexity, phaseName),
					Confidence:  "low",
					Occurrences: cpm.Runs,
					Suggestion:  fmt.Sprintf("Consider reducing retry budget for %s complexity", complexity),
				})
			}
		}
	}

	// loop_model_affinity: high loop detection rate for a runtime
	for rtKey, rm := range a.RuntimePerformance {
		if rm.Tasks < m.MinOccurrences {
			continue
		}
		// Check all phase metrics for loop detections attributed to this runtime
		// (simplified: look at overall failure rate as proxy)
		if rm.SuccessRate < 0.5 && rm.Failures >= m.MinOccurrences {
			patterns = append(patterns, Pattern{
				Type:        "loop_model_affinity",
				Description: fmt.Sprintf("%s has %.0f%% failure rate across %d tasks", rtKey, (1-rm.SuccessRate)*100, rm.Tasks),
				Confidence:  "medium",
				Occurrences: rm.Failures,
				Suggestion:  fmt.Sprintf("Consider replacing %s with a better-performing runtime/model", rtKey),
			})
		}
	}

	// timeout_mismatch: significant timeout rate for a phase
	for phaseName, pm := range a.PhaseMetrics {
		if pm.TotalRuns >= m.MinOccurrences && pm.Timeouts > 0 {
			timeoutRate := float64(pm.Timeouts) / float64(pm.TotalRuns)
			if timeoutRate > 0.2 {
				patterns = append(patterns, Pattern{
					Type:        "timeout_mismatch",
					Description: fmt.Sprintf("%s phase times out %.0f%% of the time", phaseName, timeoutRate*100),
					Confidence:  "medium",
					Occurrences: pm.Timeouts,
					Suggestion:  "Increase absolute_timeout for this phase/complexity",
				})
			}
		}
	}

	for complexity, co := range a.ComplexityOutcomes {
		if co.Tasks < m.MinOccurrences {
			continue
		}
		co.L2Rate = float64(co.L2Cycles) / float64(co.Tasks)
		if co.L2Rate > 0.4 {
			conf := "medium"
			if co.L3Cycles > 0 {
				conf = "high"
			}
			patterns = append(patterns, Pattern{
				Type:        "complexity_mismatch",
				Description: fmt.Sprintf("%s tasks trigger L2 cycles %.0f%% of the time", complexity, co.L2Rate*100),
				Confidence:  conf,
				Occurrences: co.L2Cycles,
				Suggestion:  fmt.Sprintf("Bump default complexity above %s or review task specs", complexity),
			})
		}
	}

	return patterns
}

func (m *Miner) readEvents(cutoff time.Time) ([]rawEvent, error) {
	var all []rawEvent
	for _, path := range logPaths(m.EventLogPath) {
		events, err := readEventsFromFile(path, cutoff)
		if err != nil {
			continue
		}
		all = append(all, events...)
	}
	return all, nil
}

func readEventsFromFile(path string, cutoff time.Time) ([]rawEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []rawEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var ev rawEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if !ev.Timestamp.IsZero() && ev.Timestamp.Before(cutoff) {
			continue
		}
		events = append(events, ev)
	}
	return events, scanner.Err()
}

func logPaths(base string) []string {
	paths := []string{}
	for i := 3; i >= 1; i-- {
		p := fmt.Sprintf("%s.%d", base, i)
		if _, err := os.Stat(p); err == nil {
			paths = append(paths, p)
		}
	}
	paths = append(paths, base)
	return paths
}

func (m *Miner) ensureRuntime(a *Analysis, key string) *RuntimeMetrics {
	if rm, ok := a.RuntimePerformance[key]; ok {
		return rm
	}
	rm := &RuntimeMetrics{ByDomain: make(map[string]*DomainMetrics)}
	a.RuntimePerformance[key] = rm
	return rm
}

func (m *Miner) ensureDomain(rm *RuntimeMetrics, domain string) *DomainMetrics {
	if dm, ok := rm.ByDomain[domain]; ok {
		return dm
	}
	dm := &DomainMetrics{}
	rm.ByDomain[domain] = dm
	return dm
}

func (m *Miner) ensurePhase(a *Analysis, name string) *PhaseMetric {
	if pm, ok := a.PhaseMetrics[name]; ok {
		return pm
	}
	pm := &PhaseMetric{ByComplexity: make(map[string]*ComplexityPhaseMetric)}
	a.PhaseMetrics[name] = pm
	return pm
}

func (m *Miner) ensureComplexityPhase(pm *PhaseMetric, complexity string) *ComplexityPhaseMetric {
	if cpm, ok := pm.ByComplexity[complexity]; ok {
		return cpm
	}
	cpm := &ComplexityPhaseMetric{}
	pm.ByComplexity[complexity] = cpm
	return cpm
}

func (m *Miner) ensureComplexityOutcome(a *Analysis, complexity string) *ComplexityOutcome {
	if co, ok := a.ComplexityOutcomes[complexity]; ok {
		return co
	}
	co := &ComplexityOutcome{}
	a.ComplexityOutcomes[complexity] = co
	return co
}

func (m *Miner) extractDomain(ev rawEvent) string {
	if d, ok := ev.Data["domain"].(string); ok {
		return d
	}
	return ""
}

func (m *Miner) extractString(ev rawEvent, key string) string {
	if s, ok := ev.Data[key].(string); ok {
		return s
	}
	return ""
}

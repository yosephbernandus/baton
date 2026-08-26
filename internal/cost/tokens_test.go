package cost

import (
	"math"
	"testing"
	"time"
)

func approx(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestEstimateFromTokensPricesInputAndOutput(t *testing.T) {
	// sonnet: $3/M in, $15/M out.
	got := EstimateFromTokens("sonnet", Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	approx(t, got, 18, "sonnet 1M in + 1M out")
}

// Reported input totals include cached reads, so billing both would charge the
// cached tokens twice.
func TestCachedReadsAreNotChargedTwice(t *testing.T) {
	// sonnet: $3/M in, $0.30/M cached.
	// 1M input of which 800k cached → 200k at 3 + 800k at 0.30.
	got := EstimateFromTokens("sonnet", Usage{InputTokens: 1_000_000, CachedReadTokens: 800_000})
	approx(t, got, 0.2*3+0.8*0.3, "sonnet with cached reads")

	full := EstimateFromTokens("sonnet", Usage{InputTokens: 1_000_000})
	if got >= full {
		t.Errorf("cached reads cost %v, want less than the uncached %v", got, full)
	}
}

// A model with no cached rate bills cached reads at the input rate rather than
// dropping them.
func TestCachedReadsFallBackToInputRate(t *testing.T) {
	got := EstimateFromTokens("gpt-4o", Usage{InputTokens: 1_000_000, CachedReadTokens: 500_000})
	approx(t, got, 2.5, "gpt-4o ignores an absent cached rate")
}

// Cached counts larger than the input total must not produce a negative charge.
func TestCachedReadsExceedingInputDoNotGoNegative(t *testing.T) {
	got := EstimateFromTokens("sonnet", Usage{InputTokens: 100, CachedReadTokens: 5000})
	if got < 0 {
		t.Errorf("estimate=%v, want no negative charge", got)
	}
}

// An unknown model must cost something, or it looks free next to a priced one.
func TestUnknownModelIsNotFree(t *testing.T) {
	rate, known := RateFor("some-model-nobody-configured")
	if known {
		t.Fatal("expected the model to be unknown")
	}
	if rate.InputPerM <= 0 || rate.OutputPerM <= 0 {
		t.Errorf("default rate=%+v, want a non-zero fallback", rate)
	}
	if got := EstimateFromTokens("some-model-nobody-configured", Usage{OutputTokens: 1_000_000}); got <= 0 {
		t.Errorf("estimate=%v, want a non-zero charge", got)
	}
}

func TestConfiguredRatesOverrideDefaults(t *testing.T) {
	before, _ := RateFor("sonnet")
	t.Cleanup(func() { SetTokenRates(map[string]TokenRate{"sonnet": before}) })

	SetTokenRates(map[string]TokenRate{"sonnet": {InputPerM: 1, OutputPerM: 2}})
	approx(t, EstimateFromTokens("sonnet", Usage{InputTokens: 1_000_000}), 1, "overridden input rate")
}

// Reported tokens are the better figure, so an entry carrying them must not be
// priced from how long it ran.
func TestRecordPrefersReportedTokensOverElapsedTime(t *testing.T) {
	tr, err := NewTracker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Record(Entry{
		TaskID: "t1", Runtime: "acp", Model: "sonnet",
		Duration: 10 * time.Minute,
		Usage:    &Usage{InputTokens: 1_000_000, OutputTokens: 0},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := tr.ReadAll()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	e := entries[0]
	if e.Source != SourceMeasured {
		t.Errorf("source=%q, want %q", e.Source, SourceMeasured)
	}
	approx(t, e.Estimate, 3, "priced from tokens, not from ten minutes elapsed")
}

func TestRecordFallsBackToElapsedWhenNoTokensReported(t *testing.T) {
	tr, err := NewTracker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Record(Entry{
		TaskID: "t1", Runtime: "exec", Model: "sonnet", Duration: 2 * time.Minute,
	}); err != nil {
		t.Fatal(err)
	}

	entries, _ := tr.ReadAll()
	if entries[0].Source != SourceElapsed {
		t.Errorf("source=%q, want %q", entries[0].Source, SourceElapsed)
	}
	if entries[0].Estimate <= 0 {
		t.Error("elapsed-time fallback produced no figure")
	}
}

// A usage report of all zeros means the runtime said nothing useful, not that
// the turn was free.
func TestZeroUsageIsTreatedAsUnreported(t *testing.T) {
	tr, err := NewTracker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Record(Entry{
		TaskID: "t1", Runtime: "acp", Model: "sonnet",
		Duration: time.Minute, Usage: &Usage{},
	}); err != nil {
		t.Fatal(err)
	}

	entries, _ := tr.ReadAll()
	if entries[0].Source != SourceElapsed {
		t.Errorf("source=%q, want %q — an empty report is not a free turn", entries[0].Source, SourceElapsed)
	}
	if entries[0].Estimate <= 0 {
		t.Error("estimate=0, want the elapsed-time fallback to apply")
	}
}

// The summary must let a reader tell how much of the total was counted.
func TestSummarySeparatesMeasuredFromInferred(t *testing.T) {
	tr, err := NewTracker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Record(Entry{TaskID: "a", Model: "sonnet", Runtime: "acp",
		Usage: &Usage{InputTokens: 1_000_000}})
	_ = tr.Record(Entry{TaskID: "b", Model: "sonnet", Runtime: "exec",
		Duration: 3 * time.Minute})

	s, err := tr.Summarize()
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalTasks != 2 {
		t.Fatalf("TotalTasks=%d, want 2", s.TotalTasks)
	}
	if s.MeasuredTasks != 1 {
		t.Errorf("MeasuredTasks=%d, want 1", s.MeasuredTasks)
	}
	approx(t, s.MeasuredEstimate, 3, "measured share")
	if s.MeasuredEstimate >= s.TotalEstimate {
		t.Error("measured share should be less than the total when one task was inferred")
	}
	if s.InputTokens != 1_000_000 {
		t.Errorf("InputTokens=%d, want 1000000", s.InputTokens)
	}
}

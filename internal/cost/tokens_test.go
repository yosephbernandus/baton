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

// Input and cached reads are disjoint counters: input is what was sent fresh,
// cached is what came from a prompt cache. Measured against OpenCode, total ==
// input + output + cached exactly.
//
// This started out assuming input included cached. That under-billed and made
// cache rates read above 100% — a real pipeline reported 3210%.
func TestInputAndCachedReadsArePricedSeparately(t *testing.T) {
	// sonnet: $3/M in, $0.30/M cached.
	got := EstimateFromTokens("sonnet", Usage{InputTokens: 200_000, CachedReadTokens: 800_000})
	approx(t, got, 0.2*3+0.8*0.3, "sonnet with cached reads")

	// Cached reads are additional input, so they cost more than not having them.
	bare := EstimateFromTokens("sonnet", Usage{InputTokens: 200_000})
	if got <= bare {
		t.Errorf("with cached reads %v, want more than without %v — they are extra input", got, bare)
	}
	// And they are cheaper than the same volume sent fresh.
	allFresh := EstimateFromTokens("sonnet", Usage{InputTokens: 1_000_000})
	if got >= allFresh {
		t.Errorf("cached %v, want less than the same volume fresh %v", got, allFresh)
	}
}

// A model with no cached rate bills cached reads at the input rate rather than
// treating them as free.
func TestCachedReadsFallBackToInputRate(t *testing.T) {
	got := EstimateFromTokens("gpt-4o", Usage{InputTokens: 1_000_000, CachedReadTokens: 500_000})
	approx(t, got, 1.5*2.5, "gpt-4o bills cached reads at the input rate")
}

// Cache rate is cached over all input, so it can never exceed 100%.
func TestCacheRateCannotExceedOneHundredPercent(t *testing.T) {
	e := Entry{Usage: &Usage{InputTokens: 144, CachedReadTokens: 9536}}
	rate := e.CacheRate()
	if rate < 0 || rate > 1 {
		t.Fatalf("rate=%v, want a share between 0 and 1", rate)
	}
	approx(t, rate, 9536.0/(144.0+9536.0), "phase 8 of a measured pipeline run")
}

// A turn that was served entirely from cache still counts as measured.
func TestAllCachedTurnIsStillMeasured(t *testing.T) {
	tr, err := NewTracker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Record(Entry{
		TaskID: "t1", Model: "sonnet", Runtime: "acp",
		Duration: time.Minute, Usage: &Usage{CachedReadTokens: 9536},
	}); err != nil {
		t.Fatal(err)
	}
	entries, _ := tr.ReadAll()
	if entries[0].Source != SourceMeasured {
		t.Errorf("source=%q, want %q", entries[0].Source, SourceMeasured)
	}
	if entries[0].Estimate <= 0 {
		t.Error("estimate=0, want cached reads to cost something")
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
	if rate := s.CacheRate(); rate != 0 {
		t.Errorf("CacheRate=%v, want 0 when nothing was cached", rate)
	}
}

// A workload can be almost entirely cached — a measured pipeline run came back
// 98% cached — so an unknown model's fallback must price cached reads as cached,
// not as fresh input.
func TestUnknownModelPricesCachedReadsAsCached(t *testing.T) {
	rate, known := RateFor("some-model-nobody-configured")
	if known {
		t.Fatal("expected the model to be unknown")
	}
	if rate.CachedPerM <= 0 {
		t.Fatalf("default rate=%+v, want a cached rate below the input rate", rate)
	}
	if rate.CachedPerM >= rate.InputPerM {
		t.Errorf("cached rate %v, want it below the input rate %v", rate.CachedPerM, rate.InputPerM)
	}

	// The realistic shape: a little fresh input, a lot of cache.
	u := Usage{InputTokens: 213, CachedReadTokens: 9408, OutputTokens: 253}
	got := EstimateFromTokens("some-model-nobody-configured", u)
	asFresh := EstimateFromTokens("some-model-nobody-configured",
		Usage{InputTokens: u.InputTokens + u.CachedReadTokens, OutputTokens: u.OutputTokens})
	if got >= asFresh {
		t.Errorf("cached-heavy turn cost %v, want less than %v if it were all fresh", got, asFresh)
	}
}

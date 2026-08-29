package gateway

import (
	"strings"
	"testing"

	"github.com/yosephbernandus/baton/internal/transport"
)

func capsLookup(c transport.Caps) func(string) (transport.Caps, bool) {
	return func(string) (transport.Caps, bool) { return c, true }
}

func findingFor(t *testing.T, findings []Finding, roleName string) (Finding, bool) {
	t.Helper()
	for _, f := range findings {
		if strings.Contains(f.Message, `"`+roleName+`"`) {
			return f, true
		}
	}
	return Finding{}, false
}

// reviewer is read-only: Read, Grep, Glob, Bash.
// developer declares no boundary at all.
var readOnlyRole = map[string]string{"reviewer": "mock"}

func TestPerToolRestrictionReportsNothing(t *testing.T) {
	findings := CheckRoleCapabilities(readOnlyRole, capsLookup(transport.Caps{
		Probed: true, ToolRestriction: transport.RestrictPerTool,
	}))
	if len(findings) != 0 {
		t.Errorf("findings=%v, want none when the boundary can be enforced exactly", findings)
	}
}

// A read-only role on a transport that cannot restrict anything loses the most:
// the run reports success while the boundary did nothing. It is still only a
// warning, because gateway errors block a run and this does not stop the work —
// it weakens a guarantee. Blocking would also fail every existing strict-mode
// pipeline on upgrade, since no runtime in the shipped example config declares a
// restriction flag.
func TestNoRestrictionOnReadOnlyRoleWarnsWithoutBlocking(t *testing.T) {
	findings := CheckRoleCapabilities(readOnlyRole, capsLookup(transport.Caps{
		Probed: true, ToolRestriction: transport.RestrictNone,
	}))
	f, ok := findingFor(t, findings, "reviewer")
	if !ok {
		t.Fatalf("findings=%v, want one naming reviewer", findings)
	}
	if f.Severity != SeverityWarn {
		t.Errorf("severity=%v, want warn — an unenforced boundary must not block a run", f.Severity)
	}
	if !strings.Contains(f.Message, "prompt guidance only") {
		t.Errorf("message=%q, want it to say the boundary is unenforced", f.Message)
	}
}

// A coarse mechanism is a single switch: no edits, and in practice no commands
// either. test_lead permits Read, Grep and Glob and nothing else, so the switch
// expresses its boundary exactly.
func TestCoarseRestrictionCoversARoleThatWantsNeither(t *testing.T) {
	findings := CheckRoleCapabilities(map[string]string{"test_lead": "mock"}, capsLookup(transport.Caps{
		Probed: true, ToolRestriction: transport.RestrictCoarse,
	}))
	if len(findings) != 0 {
		t.Errorf("findings=%v, want none: the switch matches this boundary exactly", findings)
	}
}

// reviewer is read-only about files but runs commands to verify. A coarse mode
// withholds both, so applying it exceeds the boundary — asking for it cost a
// completion phase its build check and blocked a pipeline. The transport
// declines to apply it there, so the gap is reported.
func TestCoarseRestrictionDoesNotFitARoleThatRunsCommands(t *testing.T) {
	findings := CheckRoleCapabilities(readOnlyRole, capsLookup(transport.Caps{
		Probed: true, ToolRestriction: transport.RestrictCoarse,
	}))
	f, ok := findingFor(t, findings, "reviewer")
	if !ok {
		t.Fatalf("findings=%v, want one naming reviewer", findings)
	}
	if f.Severity != SeverityWarn {
		t.Errorf("severity=%v, want warn", f.Severity)
	}
	if !strings.Contains(f.Message, "does not fit") {
		t.Errorf("message=%q, want it to say the mechanism does not fit", f.Message)
	}
}

// tester permits Edit, Write and Bash. A coarse toggle cannot express that
// either, so the gap is reported even though some restriction exists.
func TestCoarseRestrictionCannotExpressAMixedBoundary(t *testing.T) {
	findings := CheckRoleCapabilities(map[string]string{"tester": "mock"}, capsLookup(transport.Caps{
		Probed: true, ToolRestriction: transport.RestrictCoarse,
	}))
	f, ok := findingFor(t, findings, "tester")
	if !ok {
		t.Fatalf("findings=%v, want one naming tester", findings)
	}
	if f.Severity != SeverityWarn {
		t.Errorf("severity=%v, want warn", f.Severity)
	}
}

// A role that asks for no boundary has nothing to enforce, so no transport can
// fail it.
func TestRoleWithoutABoundaryIsNeverReported(t *testing.T) {
	findings := CheckRoleCapabilities(map[string]string{"developer": "mock"}, capsLookup(transport.Caps{
		Probed: true, ToolRestriction: transport.RestrictNone,
	}))
	if len(findings) != 0 {
		t.Errorf("findings=%v, want none: developer declares no boundary", findings)
	}
}

// Unprobed capabilities are not evidence of incapability. Reporting them as a
// failure would flag every ACP runtime that could not be reached at preflight.
func TestUnprobedCapabilitiesWarnRatherThanFail(t *testing.T) {
	findings := CheckRoleCapabilities(readOnlyRole, capsLookup(transport.Caps{
		Probed: false, ToolRestriction: transport.RestrictNone,
	}))
	f, ok := findingFor(t, findings, "reviewer")
	if !ok {
		t.Fatalf("findings=%v, want one naming reviewer", findings)
	}
	if f.Severity != SeverityWarn {
		t.Errorf("severity=%v, want warn — not established is not the same as cannot", f.Severity)
	}
	if !strings.Contains(f.Message, "unverified") {
		t.Errorf("message=%q, want it to say the boundary is unverified", f.Message)
	}
}

// A runtime the lookup has no answer for is skipped, not guessed at.
func TestUnknownRuntimeIsSkipped(t *testing.T) {
	findings := CheckRoleCapabilities(readOnlyRole, func(string) (transport.Caps, bool) {
		return transport.Caps{}, false
	})
	if len(findings) != 0 {
		t.Errorf("findings=%v, want none when capabilities are unknown", findings)
	}
}

func TestNilLookupSkipsTheCheck(t *testing.T) {
	if findings := CheckRoleCapabilities(readOnlyRole, nil); findings != nil {
		t.Errorf("findings=%v, want nil", findings)
	}
}

// Every role reported once, in a stable order, so the report does not shuffle
// between runs.
func TestFindingsAreStablyOrdered(t *testing.T) {
	all := map[string]string{"reviewer": "mock", "lead": "mock", "test_lead": "mock"}
	lookup := capsLookup(transport.Caps{Probed: true, ToolRestriction: transport.RestrictNone})

	first := CheckRoleCapabilities(all, lookup)
	if len(first) != 3 {
		t.Fatalf("got %d findings, want one per role with a boundary", len(first))
	}
	for i := 0; i < 5; i++ {
		again := CheckRoleCapabilities(all, lookup)
		for j := range first {
			if again[j].Message != first[j].Message {
				t.Fatalf("finding order changed between runs at %d", j)
			}
		}
	}
}

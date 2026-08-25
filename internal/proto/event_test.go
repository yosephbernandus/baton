package proto

import "testing"

func eventsFrom(lines ...string) []Event {
	var evs []Event
	for _, l := range lines {
		if mk, ok := ParseMarker(l); ok {
			evs = append(evs, MarkerEvent(mk, l))
		}
	}
	return evs
}

func TestNotesAndErrorsPreserveOrder(t *testing.T) {
	evs := eventsFrom(
		"BATON:H:working",
		"BATON:N:tried X",
		"BATON:E:compile failed",
		"BATON:N:Y also failed",
		"BATON:C:setup:done",
	)

	notes := Notes(evs)
	if len(notes) != 2 || notes[0] != "tried X" || notes[1] != "Y also failed" {
		t.Fatalf("notes=%q", notes)
	}
	errs := Errors(evs)
	if len(errs) != 1 || errs[0] != "compile failed" {
		t.Fatalf("errors=%q", errs)
	}
}

func TestCountHeartbeats(t *testing.T) {
	evs := eventsFrom(
		"BATON:H:working",
		"BATON:P:50:progress",
		"BATON:H:still going",
		"BATON:H:almost done",
	)
	if c := CountHeartbeats(evs); c != 3 {
		t.Fatalf("count=%d, want 3", c)
	}
}

// A zero Marker reads as MarkerHeartbeat because that constant is 0. Events
// carrying no marker (a structured transport reporting only a tool call) must
// therefore never be counted.
func TestMarkerlessEventIsNotAHeartbeat(t *testing.T) {
	evs := []Event{
		{ToolCall: &ToolCall{ID: "call_1", Name: "write", Kind: "edit"}},
		{Usage: &Usage{TotalTokens: 8693}},
	}
	if c := CountHeartbeats(evs); c != 0 {
		t.Fatalf("count=%d, want 0 — markerless events must not read as heartbeats", c)
	}
	if n := Notes(evs); n != nil {
		t.Fatalf("notes=%q, want nil", n)
	}
	if _, ok := LastCompletion(evs, "setup"); ok {
		t.Fatal("markerless events must not yield a completion")
	}
}

func TestLastCompletionTakesMostRecentMatch(t *testing.T) {
	evs := eventsFrom(
		"BATON:C:setup:fail:first try",
		"BATON:C:other:done",
		"BATON:C:setup:done:second try",
	)
	cp, ok := LastCompletion(evs, "setup")
	if !ok {
		t.Fatal("expected a completion for setup")
	}
	if cp.Status != "done" || cp.Detail != "second try" {
		t.Fatalf("cp=%+v, want the later setup completion", cp)
	}
}

func TestLastCompletionIgnoresOtherSignals(t *testing.T) {
	evs := eventsFrom("BATON:C:other:done", "BATON:N:note")
	if _, ok := LastCompletion(evs, "setup"); ok {
		t.Fatal("completion for a different phase must not match")
	}
}

func TestMarkerEventKeepsRawLine(t *testing.T) {
	evs := eventsFrom("prefix BATON:N:hello")
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Raw != "prefix BATON:N:hello" {
		t.Fatalf("raw=%q, want the full source line", evs[0].Raw)
	}
	if evs[0].Marker == nil || evs[0].Marker.Msg != "hello" {
		t.Fatalf("marker=%+v", evs[0].Marker)
	}
}

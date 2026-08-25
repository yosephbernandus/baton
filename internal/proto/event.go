package proto

// Event is the transport-neutral unit the pipeline reasons about.
//
// Every transport (subprocess + stdout markers, ACP, anything later) produces
// Events. The pipeline consumes Events and never re-parses raw output, so a
// transport that has no stdout at all still drives the phase machine.
//
// Every field is a pointer because nil means "this transport does not report
// that", which is different from reporting a zero value. Marker in particular
// must stay a pointer: MarkerHeartbeat is 0, so an inlined zero Marker would
// silently read as a heartbeat.
type Event struct {
	Marker   *Marker
	ToolCall *ToolCall
	Usage    *Usage

	// Raw is the source line the marker was parsed from. Kept for loop
	// detection and debugging; transports without a text stream leave it empty.
	Raw string
}

// ToolCall describes a tool invocation a transport observed. Transports that
// only see stdout cannot fill this in; structured protocols can.
type ToolCall struct {
	ID        string
	Name      string
	Kind      string // edit | execute | read | search | other
	Status    string // pending | in_progress | completed | failed
	Locations []string
}

// Usage carries token accounting when the transport reports it.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CachedReadTokens int
	TotalTokens      int
}

// MarkerEvent builds an Event from a parsed marker and its source line.
func MarkerEvent(mk Marker, raw string) Event {
	return Event{Marker: &mk, Raw: raw}
}

// Notes returns the messages of every note marker, in order.
func Notes(events []Event) []string {
	return msgsOfType(events, MarkerNote)
}

// Errors returns the messages of every error marker, in order.
func Errors(events []Event) []string {
	return msgsOfType(events, MarkerError)
}

// CountHeartbeats returns how many heartbeat markers the transport reported.
func CountHeartbeats(events []Event) int {
	count := 0
	for _, ev := range events {
		if ev.Marker != nil && ev.Marker.Type == MarkerHeartbeat {
			count++
		}
	}
	return count
}

// LastCompletion returns the most recent completion promise matching
// expectedSignal. Scanning backwards matches the old stdout behaviour: a worker
// that retries within one attempt reports the final outcome last.
func LastCompletion(events []Event, expectedSignal string) (CompletionPromise, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		mk := events[i].Marker
		if mk == nil || mk.Type != MarkerComplete {
			continue
		}
		cp, ok := ParseCompletion(*mk)
		if !ok {
			continue
		}
		if cp.Phase == expectedSignal {
			return cp, true
		}
	}
	return CompletionPromise{}, false
}

func msgsOfType(events []Event, t MarkerType) []string {
	var out []string
	for _, ev := range events {
		if ev.Marker != nil && ev.Marker.Type == t {
			out = append(out, ev.Marker.Msg)
		}
	}
	return out
}

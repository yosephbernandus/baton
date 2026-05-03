package proto

import (
	"strconv"
	"strings"
)

type MarkerType int

const (
	MarkerHeartbeat MarkerType = iota
	MarkerProgress
	MarkerStuck
	MarkerError
	MarkerMilestone
)

type Marker struct {
	Type MarkerType
	Msg  string
	Pct  int
}

const markerPrefix = "BATON:"

func ParseMarker(line string) (Marker, bool) {
	idx := strings.Index(line, markerPrefix)
	if idx < 0 {
		return Marker{}, false
	}
	rest := line[idx+len(markerPrefix):]
	if len(rest) < 2 || rest[1] != ':' {
		return Marker{}, false
	}

	typeChar := rest[0]
	payload := rest[2:]

	switch typeChar {
	case 'H':
		return Marker{Type: MarkerHeartbeat, Msg: payload}, true
	case 'P':
		pct, msg := parseProgress(payload)
		return Marker{Type: MarkerProgress, Msg: msg, Pct: pct}, true
	case 'S':
		return Marker{Type: MarkerStuck, Msg: payload}, true
	case 'E':
		return Marker{Type: MarkerError, Msg: payload}, true
	case 'M':
		return Marker{Type: MarkerMilestone, Msg: payload}, true
	default:
		return Marker{}, false
	}
}

func parseProgress(payload string) (int, string) {
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return 0, payload
	}
	pct, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, payload
	}
	return pct, parts[1]
}

func (t MarkerType) String() string {
	switch t {
	case MarkerHeartbeat:
		return "heartbeat"
	case MarkerProgress:
		return "progress"
	case MarkerStuck:
		return "stuck"
	case MarkerError:
		return "error"
	case MarkerMilestone:
		return "milestone"
	default:
		return "unknown"
	}
}

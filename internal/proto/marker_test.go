package proto

import "testing"

func TestParseMarker(t *testing.T) {
	tests := []struct {
		line    string
		wantOk  bool
		wantTyp MarkerType
		wantMsg string
		wantPct int
	}{
		{"BATON:H:alive", true, MarkerHeartbeat, "alive", 0},
		{"BATON:P:30:implementing auth", true, MarkerProgress, "implementing auth", 30},
		{"BATON:P:100:done", true, MarkerProgress, "done", 100},
		{"BATON:S:which schema v1 or v2?", true, MarkerStuck, "which schema v1 or v2?", 0},
		{"BATON:E:build failed:undefined ref", true, MarkerError, "build failed:undefined ref", 0},
		{"BATON:M:tests passing", true, MarkerMilestone, "tests passing", 0},
		{"some output BATON:H:alive", true, MarkerHeartbeat, "alive", 0},
		{"not a marker", false, 0, "", 0},
		{"BATON:", false, 0, "", 0},
		{"BATON:X:unknown type", false, 0, "", 0},
		{"", false, 0, "", 0},
	}

	for _, tt := range tests {
		mk, ok := ParseMarker(tt.line)
		if ok != tt.wantOk {
			t.Errorf("ParseMarker(%q) ok=%v, want %v", tt.line, ok, tt.wantOk)
			continue
		}
		if !ok {
			continue
		}
		if mk.Type != tt.wantTyp {
			t.Errorf("ParseMarker(%q) type=%v, want %v", tt.line, mk.Type, tt.wantTyp)
		}
		if mk.Msg != tt.wantMsg {
			t.Errorf("ParseMarker(%q) msg=%q, want %q", tt.line, mk.Msg, tt.wantMsg)
		}
		if mk.Pct != tt.wantPct {
			t.Errorf("ParseMarker(%q) pct=%d, want %d", tt.line, mk.Pct, tt.wantPct)
		}
	}
}

func TestMarkerToMessage(t *testing.T) {
	mk := Marker{Type: MarkerProgress, Msg: "auth", Pct: 30}
	msg := MarkerToMessage(mk)
	if msg.M != "progress" {
		t.Errorf("M=%q, want progress", msg.M)
	}
	if msg.P != 30 {
		t.Errorf("P=%d, want 30", msg.P)
	}
	if msg.Msg != "auth" {
		t.Errorf("Msg=%q, want auth", msg.Msg)
	}
}

func TestEncodeDecode(t *testing.T) {
	orig := Message{M: "guide", ID: 1, Msg: "use v2", From: "human"}
	data, err := Encode(orig)
	if err != nil {
		t.Fatal(err)
	}
	if data[len(data)-1] != '\n' {
		t.Error("Encode should append newline")
	}

	decoded, err := Decode(data[:len(data)-1])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.M != orig.M || decoded.ID != orig.ID || decoded.Msg != orig.Msg || decoded.From != orig.From {
		t.Errorf("Decode mismatch: got %+v, want %+v", decoded, orig)
	}
}

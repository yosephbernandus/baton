package proto

import (
	"encoding/json"
)

type Message struct {
	M      string   `json:"m"`
	ID     int      `json:"id,omitempty"`
	Msg    string   `json:"msg,omitempty"`
	P      int      `json:"p,omitempty"`
	Detail string   `json:"detail,omitempty"`
	Dur    string   `json:"dur,omitempty"`
	Code   int      `json:"code,omitempty"`
	From   string   `json:"from,omitempty"`
	Files  []string `json:"files,omitempty"`

	ReplyTo interface{} `json:"-"`
}

func MarkerToMessage(mk Marker) Message {
	msg := Message{
		M:   mk.Type.String(),
		Msg: mk.Msg,
	}
	if mk.Type == MarkerProgress {
		msg.P = mk.Pct
	}
	return msg
}

func Encode(msg Message) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Decode(line []byte) (Message, error) {
	var msg Message
	err := json.Unmarshal(line, &msg)
	return msg, err
}

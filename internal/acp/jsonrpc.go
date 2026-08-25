package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC 2.0 over newline-delimited stdio, the framing ACP uses.
//
// The connection is bidirectional: baton sends initialize/session.* and the
// agent sends session/update notifications and session/request_permission
// requests back over the same pipe.

const jsonrpcVersion = "2.0"

// Standard JSON-RPC error codes. MethodNotFound is the one that matters in
// practice: clients probe agents with off-spec liveness methods, and answering
// -32601 is the correct reply, not a failure to log loudly.
const (
	codeParseError     = -32700
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// Handler answers inbound traffic from the agent. Returning a nil result for a
// request the handler does not recognise makes the connection reply
// -32601, which is what an off-spec probe should receive.
type Handler interface {
	// Notify handles a notification. Errors are advisory; there is nobody to
	// report them to on the wire.
	Notify(method string, params json.RawMessage)
	// Request handles a call that expects a reply. Returning errMethodNotFound
	// produces a -32601 response.
	Request(ctx context.Context, method string, params json.RawMessage) (any, error)
}

var errMethodNotFound = &rpcError{Code: codeMethodNotFound, Message: "method not found"}

// Conn is one JSON-RPC connection over a pair of streams.
type Conn struct {
	w   io.Writer
	r   io.Reader
	h   Handler
	log func(string, ...any)

	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan *message

	closed  chan struct{}
	closeMu sync.Mutex
	readErr error
}

// NewConn wires a handler to a stream pair. Serve must be running for calls to
// complete.
func NewConn(r io.Reader, w io.Writer, h Handler, log func(string, ...any)) *Conn {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Conn{
		r:       r,
		w:       w,
		h:       h,
		log:     log,
		nextID:  1,
		pending: make(map[int64]chan *message),
		closed:  make(chan struct{}),
	}
}

// Serve reads until the stream ends or ctx is cancelled. It returns the read
// error, if any; a clean EOF returns nil.
func (c *Conn) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(c.r)
	// Agent turns carry whole tool inputs and outputs on one line, so the
	// default 64KB token limit is not enough.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			c.shutdown(ctx.Err())
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var m message
		if err := json.Unmarshal(line, &m); err != nil {
			c.log("acp: undecodable frame: %v", err)
			c.replyError(nil, codeParseError, "parse error")
			continue
		}
		c.dispatch(ctx, &m)
	}

	err := scanner.Err()
	c.shutdown(err)
	return err
}

func (c *Conn) dispatch(ctx context.Context, m *message) {
	// A frame with a method is inbound work; one without is a reply to
	// something we sent.
	if m.Method == "" {
		c.deliver(m)
		return
	}
	if m.ID == nil {
		if c.h != nil {
			c.h.Notify(m.Method, m.Params)
		}
		return
	}
	go c.answer(ctx, m)
}

func (c *Conn) answer(ctx context.Context, m *message) {
	if c.h == nil {
		c.replyError(m.ID, codeMethodNotFound, "method not found")
		return
	}
	result, err := c.h.Request(ctx, m.Method, m.Params)
	if err != nil {
		var re *rpcError
		if e, ok := err.(*rpcError); ok {
			re = e
		} else {
			re = &rpcError{Code: codeInternalError, Message: err.Error()}
		}
		c.replyError(m.ID, re.Code, re.Message)
		return
	}
	c.replyResult(m.ID, result)
}

func (c *Conn) deliver(m *message) {
	if m.ID == nil {
		c.log("acp: reply with no id, dropped")
		return
	}
	var id int64
	if err := json.Unmarshal(*m.ID, &id); err != nil {
		c.log("acp: reply with non-numeric id, dropped")
		return
	}

	c.mu.Lock()
	ch, ok := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()

	if !ok {
		c.log("acp: reply for unknown id %d, dropped", id)
		return
	}
	ch <- m
}

// Call sends a request and waits for its reply. result may be nil to discard
// the payload.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	select {
	case <-c.closed:
		return c.closeReason()
	default:
	}

	c.mu.Lock()
	id := c.nextID
	c.nextID++
	ch := make(chan *message, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	rawID := json.RawMessage(fmt.Sprintf("%d", id))
	if err := c.write(&message{JSONRPC: jsonrpcVersion, ID: &rawID, Method: method, Params: mustRaw(params)}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case <-c.closed:
		return c.closeReason()
	case reply, ok := <-ch:
		if !ok {
			// shutdown closed the channel; the stream died mid-call.
			return c.closeReason()
		}
		if reply.Error != nil {
			return reply.Error
		}
		if result == nil || len(reply.Result) == 0 {
			return nil
		}
		return json.Unmarshal(reply.Result, result)
	}
}

// Notify sends a notification, which expects no reply.
func (c *Conn) Notify(method string, params any) error {
	return c.write(&message{JSONRPC: jsonrpcVersion, Method: method, Params: mustRaw(params)})
}

func (c *Conn) replyResult(id *json.RawMessage, result any) {
	raw := mustRaw(result)
	if raw == nil {
		raw = json.RawMessage("null")
	}
	_ = c.write(&message{JSONRPC: jsonrpcVersion, ID: id, Result: raw})
}

func (c *Conn) replyError(id *json.RawMessage, code int, msg string) {
	if id == nil {
		// Nothing to answer: a malformed notification has no reply address.
		return
	}
	_ = c.write(&message{JSONRPC: jsonrpcVersion, ID: id, Error: &rpcError{Code: code, Message: msg}})
}

func (c *Conn) write(m *message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.w.Write(data)
	return err
}

// shutdown fails every in-flight call so no caller waits on a dead stream.
func (c *Conn) shutdown(err error) {
	c.closeMu.Lock()
	select {
	case <-c.closed:
		c.closeMu.Unlock()
		return
	default:
	}
	c.readErr = err
	close(c.closed)
	c.closeMu.Unlock()

	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan *message)
	c.mu.Unlock()

	for _, ch := range pending {
		close(ch)
	}
}

func (c *Conn) closeReason() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.readErr != nil {
		return fmt.Errorf("acp connection closed: %w", c.readErr)
	}
	return fmt.Errorf("acp connection closed")
}

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

package socket

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yosephbernandus/baton/internal/proto"
)

func TestServerClientRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go srv.Accept(ctx)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Server broadcasts to client
	srv.Broadcast(proto.Message{M: "progress", P: 30, Msg: "working"})

	msg, err := client.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if msg.M != "progress" || msg.P != 30 {
		t.Errorf("got %+v, want progress p=30", msg)
	}
}

func TestClientSendServerReceive(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go srv.Accept(ctx)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// Client sends guidance
	client.Send(proto.Message{M: "guide", ID: 1, Msg: "use v2", From: "human"})

	select {
	case msg := <-srv.Incoming():
		if msg.M != "guide" || msg.ID != 1 || msg.Msg != "use v2" {
			t.Errorf("got %+v, want guide id=1 msg='use v2'", msg)
		}
		// Reply
		srv.Reply(msg, proto.Message{M: "ok", ID: 1})
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for incoming message")
	}

	// Client receives reply
	reply, err := client.Receive()
	if err != nil {
		t.Fatal(err)
	}
	if reply.M != "ok" || reply.ID != 1 {
		t.Errorf("got %+v, want ok id=1", reply)
	}
}

func TestMultipleClients(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go srv.Accept(ctx)

	c1, err := Dial(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()

	c2, err := Dial(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()

	// Small delay to let accept goroutines register
	time.Sleep(50 * time.Millisecond)

	srv.Broadcast(proto.Message{M: "stuck", Msg: "help"})

	msg1, err := c1.Receive()
	if err != nil {
		t.Fatal(err)
	}
	msg2, err := c2.Receive()
	if err != nil {
		t.Fatal(err)
	}

	if msg1.M != "stuck" || msg2.M != "stuck" {
		t.Errorf("both clients should receive stuck: c1=%+v c2=%+v", msg1, msg2)
	}
}

func TestStream(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	srv, err := NewServer(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go srv.Accept(ctx)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ch := client.Stream(ctx)

	time.Sleep(50 * time.Millisecond)

	srv.Broadcast(proto.Message{M: "heartbeat", Msg: "alive"})
	srv.Broadcast(proto.Message{M: "progress", P: 50, Msg: "half"})

	var received []proto.Message
	for i := 0; i < 2; i++ {
		select {
		case msg := <-ch:
			received = append(received, msg)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for stream message")
		}
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(received))
	}
	if received[0].M != "heartbeat" {
		t.Errorf("first message: got %s, want heartbeat", received[0].M)
	}
	if received[1].P != 50 {
		t.Errorf("second message: got pct=%d, want 50", received[1].P)
	}
}

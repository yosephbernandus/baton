package socket

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/yosephbernandus/baton/internal/proto"
)

type Server struct {
	path     string
	listener net.Listener
	clients  []net.Conn
	mu       sync.RWMutex
	incoming chan proto.Message
	done     chan struct{}
}

func NewServer(socketPath string) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	return &Server{
		path:     socketPath,
		listener: ln,
		incoming: make(chan proto.Message, 64),
		done:     make(chan struct{}),
	}, nil
}

func (s *Server) Accept(ctx context.Context) {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}

		s.mu.Lock()
		s.clients = append(s.clients, conn)
		s.mu.Unlock()

		go s.readClient(ctx, conn)
	}
}

func (s *Server) readClient(ctx context.Context, conn net.Conn) {
	defer func() {
		s.removeClient(conn)
		conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := proto.Decode(scanner.Bytes())
		if err != nil {
			continue
		}

		msg.ReplyTo = conn

		select {
		case s.incoming <- msg:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) removeClient(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.clients {
		if c == conn {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			return
		}
	}
}

func (s *Server) Broadcast(msg proto.Message) error {
	data, err := proto.Encode(msg)
	if err != nil {
		return err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, conn := range s.clients {
		_, _ = conn.Write(data)
	}
	return nil
}

func (s *Server) Incoming() <-chan proto.Message {
	return s.incoming
}

func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Server) Reply(msg proto.Message, reply proto.Message) error {
	if msg.ReplyTo == nil {
		return nil
	}
	data, err := proto.Encode(reply)
	if err != nil {
		return err
	}
	_, err = msg.ReplyTo.(net.Conn).Write(data)
	return err
}

func (s *Server) Close() error {
	err := s.listener.Close()
	s.mu.Lock()
	for _, c := range s.clients {
		_ = c.Close()
	}
	s.clients = nil
	s.mu.Unlock()
	_ = os.Remove(s.path)
	return err
}


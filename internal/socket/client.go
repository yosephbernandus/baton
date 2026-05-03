package socket

import (
	"bufio"
	"context"
	"net"

	"github.com/yosephbernandus/baton/internal/proto"
)

type Client struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

func Dial(socketPath string) (*Client, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
	}, nil
}

func (c *Client) Send(msg proto.Message) error {
	data, err := proto.Encode(msg)
	if err != nil {
		return err
	}
	_, err = c.conn.Write(data)
	return err
}

func (c *Client) Receive() (proto.Message, error) {
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return proto.Message{}, err
		}
		return proto.Message{}, net.ErrClosed
	}
	return proto.Decode(c.scanner.Bytes())
}

func (c *Client) Stream(ctx context.Context) <-chan proto.Message {
	ch := make(chan proto.Message, 64)
	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			msg, err := c.Receive()
			if err != nil {
				return
			}
			select {
			case ch <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

func (c *Client) Close() error {
	return c.conn.Close()
}

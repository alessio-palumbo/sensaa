package sensaa

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	maxMessageSize          = 64 * 1024
	defaultHandshakeTimeout = 5 * time.Second
)

// Client reads the update stream from a connected node. Reads must not be
// performed concurrently.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	mu     sync.Mutex
}

// Connect opens a stream and validates the node's protocol greeting.
func (n Node) Connect(ctx context.Context) (*Client, error) {
	connectCtx, cancel := context.WithTimeout(ctx, defaultHandshakeTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(connectCtx, "tcp", n.address)
	if err != nil {
		return nil, fmt.Errorf("connect to Sensaa node %q: %w", n.name, err)
	}
	client := &Client{conn: conn, reader: bufio.NewReaderSize(conn, maxMessageSize+1)}
	message, err := client.readMessage(connectCtx)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read Sensaa greeting: %w", err)
	}
	if message.Type != "hello" {
		_ = conn.Close()
		return nil, fmt.Errorf("expected Sensaa greeting, got message type %q", message.Type)
	}
	if message.Version != protocolVersion {
		_ = conn.Close()
		return nil, fmt.Errorf("unsupported Sensaa protocol version %d", message.Version)
	}
	if n.id != "" && message.ID != n.id {
		_ = conn.Close()
		return nil, fmt.Errorf("Sensaa node identity changed: discovered %q, connected to %q", n.id, message.ID)
	}
	return client, nil
}

// Read waits for and decodes the next complete sensor snapshot.
func (c *Client) Read(ctx context.Context) (Update, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	message, err := c.readMessage(ctx)
	if err != nil {
		return Update{}, err
	}
	update, err := updateFromWire(message)
	if err != nil {
		return Update{}, fmt.Errorf("read Sensaa update: %w", err)
	}
	return update, nil
}

func (c *Client) readMessage(ctx context.Context) (wireMessage, error) {
	if err := ctx.Err(); err != nil {
		return wireMessage{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return wireMessage{}, err
		}
	} else if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return wireMessage{}, err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetReadDeadline(time.Now()) })
	defer stop()

	line, err := c.reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxMessageSize {
		return wireMessage{}, errors.New("Sensaa message exceeds 64 KiB")
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return wireMessage{}, ctxErr
		}
		if errors.Is(err, io.EOF) {
			return wireMessage{}, io.EOF
		}
		return wireMessage{}, fmt.Errorf("read Sensaa stream: %w", err)
	}
	return decodeWireMessage(line)
}

func (c *Client) Close() error { return c.conn.Close() }

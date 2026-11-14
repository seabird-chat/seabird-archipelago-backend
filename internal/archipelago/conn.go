package archipelago

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/coder/websocket"
)

// DataPackage frames for large multiworlds run to tens of megabytes.
const readLimit = 128 << 20

// Conn is a WebSocket connection to an Archipelago server. Read must only be
// called from one goroutine; Send is safe for concurrent use.
type Conn struct {
	ws *websocket.Conn
}

// Dial connects to addr. An address with no scheme tries wss first and falls
// back to ws, since archipelago.gg serves TLS but many self-hosted servers
// don't.
func Dial(ctx context.Context, addr string) (*Conn, error) {
	if strings.Contains(addr, "://") {
		return dial(ctx, addr)
	}

	conn, err := dial(ctx, "wss://"+addr)
	if err == nil {
		return conn, nil
	}
	if ctx.Err() != nil {
		return nil, err
	}

	conn, plainErr := dial(ctx, "ws://"+addr)
	if plainErr != nil {
		return nil, errors.Join(err, plainErr)
	}
	return conn, nil
}

func dial(ctx context.Context, url string) (*Conn, error) {
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	})
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", url, err)
	}
	ws.SetReadLimit(readLimit)
	return &Conn{ws: ws}, nil
}

// Close closes the connection.
func (c *Conn) Close() error {
	return c.ws.Close(websocket.StatusNormalClosure, "")
}

// Read returns the packets from the next frame.
func (c *Conn) Read(ctx context.Context) ([]Packet, error) {
	_, data, err := c.ws.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return decodeFrame(data)
}

// Send writes packets as a single frame.
func (c *Conn) Send(ctx context.Context, packets ...any) error {
	data, err := json.Marshal(packets)
	if err != nil {
		return fmt.Errorf("encode frame: %w", err)
	}
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

func decodeFrame(data []byte) ([]Packet, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode frame: %w", err)
	}

	packets := make([]Packet, 0, len(raw))
	for _, r := range raw {
		var head struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal(r, &head); err != nil {
			return nil, fmt.Errorf("decode packet: %w", err)
		}
		packets = append(packets, Packet{Cmd: head.Cmd, Raw: r})
	}
	return packets, nil
}

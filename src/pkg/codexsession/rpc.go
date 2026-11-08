// Package codexsession reads Codex CLI sessions for superfile's sessions
// list, the way package claudesession reads Claude Code's.
//
// Codex keeps its sessions in a background server that every Codex window
// connects to (it starts it when none runs). The server says which sessions
// are loaded and what each is doing, and takes prompts and names for them;
// it speaks JSON-RPC over a WebSocket on a Unix socket in Codex's folder.
// Each session's log file (its rollout) has the conversation and the
// changes it made.
package codexsession

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // the WebSocket handshake is defined with SHA-1
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Home is Codex's folder: $CODEX_HOME, else ~/.codex.
func Home() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex")
}

// SocketPath is where the server listens, in Codex's folder home.
func SocketPath(home string) string {
	return filepath.Join(home, "app-server-control", "app-server-control.sock")
}

// ErrNoServer means no Codex server runs: no Codex session can be running.
var ErrNoServer = errors.New("no Codex server is running")

// Client is one connection to the server.
type Client struct {
	conn   net.Conn
	reader *bufio.Reader
	nextID int
}

const (
	wsOpText  = 0x1
	wsOpClose = 0x8
	wsOpPing  = 0x9
	wsOpPong  = 0xA
	wsFinBit  = 0x80
	wsMaskBit = 0x80
	// wsMaxMessage caps a message read: the server says it sends at most
	// 16 MiB unfragmented.
	wsMaxMessage = 64 << 20
	wsGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// Dial connects to the server at sock and says hello. The connection gives
// up at the context's deadline.
func Dial(ctx context.Context, sock string) (*Client, error) {
	if _, err := os.Stat(sock); err != nil {
		return nil, ErrNoServer
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", sock)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoServer, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c := &Client{conn: conn, reader: bufio.NewReader(conn)}
	if err := c.handshake(); err != nil {
		conn.Close()
		return nil, err
	}
	var hello json.RawMessage
	if err := c.Call("initialize", map[string]any{
		"clientInfo": map[string]string{"name": "marmota", "version": "1"},
	}, &hello); err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.notify("initialized"); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Close ends the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) handshake() error {
	nonce := make([]byte, 16) //nolint:mnd // the key is 16 random bytes
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	key := base64.StdEncoding.EncodeToString(nonce)
	req := "GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(c.conn, req); err != nil {
		return err
	}
	resp, err := http.ReadResponse(c.reader, nil)
	if err != nil {
		return fmt.Errorf("codex server handshake: %w", err)
	}
	resp.Body.Close()
	sum := sha1.Sum([]byte(key + wsGUID)) //nolint:gosec // defined by the protocol
	if resp.StatusCode != http.StatusSwitchingProtocols ||
		resp.Header.Get("Sec-Websocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		return fmt.Errorf("codex server handshake: %s", resp.Status)
	}
	return nil
}

// RPCError is an error the server answered with.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return e.Message }

// Call sends a request and decodes its result into out. Notifications that
// arrive meanwhile are skipped.
func (c *Client) Call(method string, params, out any) error {
	c.nextID++
	id := c.nextID
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		data, err := c.readMessage()
		if err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *RPCError       `json:"error"`
		}
		if json.Unmarshal(data, &msg) != nil || msg.ID == nil || *msg.ID != id || msg.Method != "" {
			continue // a notification, or a request to us we don't take
		}
		if msg.Error != nil {
			return fmt.Errorf("%s: %w", method, msg.Error)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(msg.Result, out)
	}
}

func (c *Client) notify(method string) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (c *Client) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.writeFrame(wsOpText, data)
}

// writeFrame writes one masked frame, as a client must.
func (c *Client) writeFrame(op byte, data []byte) error {
	var hdr bytes.Buffer
	hdr.WriteByte(wsFinBit | op)
	switch n := len(data); {
	case n < 126: //nolint:mnd // WebSocket length encodings
		hdr.WriteByte(wsMaskBit | byte(n))
	case n <= 0xFFFF:
		hdr.WriteByte(wsMaskBit | 126) //nolint:mnd // 16-bit length follows
		_ = binary.Write(&hdr, binary.BigEndian, uint16(n))
	default:
		hdr.WriteByte(wsMaskBit | 127)                      //nolint:mnd // 64-bit length follows
		_ = binary.Write(&hdr, binary.BigEndian, uint64(n)) //nolint:gosec // n is a length
	}
	mask := make([]byte, 4) //nolint:mnd // masking key
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	hdr.Write(mask)
	masked := make([]byte, len(data))
	for i, b := range data {
		masked[i] = b ^ mask[i%4]
	}
	_, err := c.conn.Write(append(hdr.Bytes(), masked...))
	return err
}

// readMessage reads one whole message, answering pings on the way.
func (c *Client) readMessage() ([]byte, error) {
	var msg []byte
	for {
		var hdr [2]byte
		if _, err := io.ReadFull(c.reader, hdr[:]); err != nil {
			return nil, err
		}
		fin, op := hdr[0]&wsFinBit != 0, hdr[0]&0x0F //nolint:mnd // opcode bits
		n := uint64(hdr[1] & 0x7F)                   //nolint:mnd // length bits
		switch n {
		case 126: //nolint:mnd // 16-bit length follows
			var l uint16
			if err := binary.Read(c.reader, binary.BigEndian, &l); err != nil {
				return nil, err
			}
			n = uint64(l)
		case 127: //nolint:mnd // 64-bit length follows
			if err := binary.Read(c.reader, binary.BigEndian, &n); err != nil {
				return nil, err
			}
		}
		if n > wsMaxMessage || uint64(len(msg))+n > wsMaxMessage {
			return nil, errors.New("codex server message too large")
		}
		var mask []byte
		if hdr[1]&wsMaskBit != 0 {
			mask = make([]byte, 4) //nolint:mnd // masking key
			if _, err := io.ReadFull(c.reader, mask); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(c.reader, payload); err != nil {
			return nil, err
		}
		if mask != nil {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		switch op {
		case wsOpPing:
			if err := c.writeFrame(wsOpPong, payload); err != nil {
				return nil, err
			}
			continue
		case wsOpPong:
			continue
		case wsOpClose:
			return nil, io.EOF
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

// connect dials the server in home with a deadline.
func connect(home string, timeout time.Duration) (*Client, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	c, err := Dial(ctx, SocketPath(home))
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return c, cancel, nil
}

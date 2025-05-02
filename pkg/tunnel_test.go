package tunnel

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func startEchoWebSocketServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		require.NoError(t, err, "websocket.Accept failed")
		defer func() {
			err := c.Close(websocket.StatusNormalClosure, "bye")
			if err != nil && err.Error() != "failed to close WebSocket: use of closed network connection" {
				t.Errorf("failed to close websocket: %v", err)
			}
		}()
		for {
			typ, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				_ = c.Write(context.Background(), websocket.MessageBinary, data)
			}
		}
	}))
}

func TestTunnelAdapterSubprotocolWithWebSocket(t *testing.T) {
	server := startEchoWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err, "websocket.Dial failed")
	defer func() {
		if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
			t.Errorf("failed to close websocket: %v", err)
		}
	}()

	protocol := NewIAPTunnelProtocol()
	payload := []byte("hello subprotocol")

	// Send a framed message using the protocol
	n, err := protocol.SendDataFrame(conn, payload)
	require.NoError(t, err, "SendDataFrame failed")
	expectedLen := len(payload)
	assert.Equal(t, expectedLen, n, "bytes written mismatch")

	// Read the echoed message and parse the subprotocol frame
	_, msg, err := conn.Read(context.Background())
	require.NoError(t, err, "websocket.Read failed")

	tag, rest, err := protocol.ExtractSubprotocolTag(msg)
	require.NoError(t, err, "ExtractSubprotocolTag failed")
	assert.Equal(t, SUBPROTOCOL_TAG_DATA, tag, "unexpected tag")

	data, _, err := protocol.ExtractSubprotocolData(rest)
	require.NoError(t, err, "ExtractSubprotocolData failed")
	assert.True(t, bytes.Equal(data, payload), "expected payload %q, got %q", payload, data)
}

func TestTunnelAdapter_ReadWrite_Close(t *testing.T) {
	server := startEchoWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err, "websocket.Dial failed")
	defer func() {
		if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
			t.Errorf("failed to close websocket: %v", err)
		}
	}()

	adapter := &TunnelAdapter{
		conn:     conn,
		protocol: NewIAPTunnelProtocol(),
		target:   TunnelTarget{},
		inbound:  make(chan []byte, 1),
		closed:   make(chan struct{}),
	}

	// Test Read with pending data
	adapter.pending = []byte("hello")
	buf := make([]byte, 10)
	n, err := adapter.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))

	// Test Read from inbound channel
	adapter.pending = nil
	go func() { adapter.inbound <- []byte("world") }()
	n, err = adapter.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "world", string(buf[:n]))

	// Test Read after closed
	close(adapter.closed)
	_, err = adapter.Read(buf)
	assert.Equal(t, io.EOF, err)
}

func TestTunnelAdapter_Write(t *testing.T) {
	server := startEchoWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err, "websocket.Dial failed")
	defer func() {
		if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
			t.Errorf("failed to close websocket: %v", err)
		}
	}()

	adapter := &TunnelAdapter{
		conn:     conn,
		protocol: NewIAPTunnelProtocol(),
		target:   TunnelTarget{},
		inbound:  make(chan []byte, 1),
		closed:   make(chan struct{}),
	}

	data := []byte("testdata")
	n, err := adapter.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)
}

func TestTunnelAdapter_Close(t *testing.T) {
	server := startEchoWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err, "websocket.Dial failed")

	adapter := &TunnelAdapter{
		conn:   conn,
		closed: make(chan struct{}),
	}
	err = adapter.Close()
	require.NoError(t, err)
	select {
	case <-adapter.closed:
	default:
		t.Error("expected closed channel to be closed")
	}
}

func TestProxyBidirectionalWithWebSocket(t *testing.T) {
	server := startEchoWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.Dial(context.Background(), wsURL, nil)
	require.NoError(t, err, "websocket.Dial failed")
	defer func() {
		if err := conn.Close(websocket.StatusNormalClosure, "bye"); err != nil {
			t.Errorf("failed to close websocket: %v", err)
		}
	}()

	adapter := &TunnelAdapter{
		conn:     conn,
		protocol: NewIAPTunnelProtocol(),
		target:   TunnelTarget{},
		inbound:  make(chan []byte, 10),
		errors:   make(chan error, 10),
		closed:   make(chan struct{}),
		ready:    make(chan struct{}),
	}

	// Start a goroutine to read from the websocket and push to inbound
	go func() {
		protocol := NewIAPTunnelProtocol()
		for {
			_, msg, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			tag, rest, err := protocol.ExtractSubprotocolTag(msg)
			if err != nil || tag != SUBPROTOCOL_TAG_DATA {
				continue
			}
			data, _, err := protocol.ExtractSubprotocolData(rest)
			if err != nil {
				continue
			}
			adapter.inbound <- data
		}
	}()

	// Create a pipe to simulate a TCP connection
	client, serverConn := net.Pipe()
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("failed to close client pipe: %v", err)
		}
		if err := serverConn.Close(); err != nil {
			t.Errorf("failed to close server pipe: %v", err)
		}
	}()

	// Write data from the client side and check echo
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := client.Write([]byte("ping"))
		require.NoError(t, err)
		buf := make([]byte, 10)
		n, err := client.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, "ping", string(buf[:n]))
	}()

	// Proxy data between the pipe and the websocket tunnel
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		_ = proxyBidirectional(ctx, serverConn, adapter)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for echo")
	}
}

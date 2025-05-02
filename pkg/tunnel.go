package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/oauth2/google"
	"golang.org/x/sync/errgroup"
	oauthsvc "google.golang.org/api/oauth2/v2"
)

// Common URL and SubProtocol constants.
const (
	URL_SCHEME               = "wss"
	URL_HOST                 = "tunnel.cloudproxy.app"
	MTLS_URL_HOST            = "mtls.tunnel.cloudproxy.app"
	URL_PATH_ROOT            = "/v4"
	CONNECT_ENDPOINT         = "connect"
	RECONNECT_ENDPOINT       = "reconnect"
	SEC_PROTOCOL_SUFFIX      = "bearer.relay.tunnel.cloudproxy.app"
	TUNNEL_CLOUDPROXY_ORIGIN = "bot:iap-tunneler"
	TUNNEL_USER_AGENT        = "go-iap-tunnel"

	SUBPROTOCOL_NAME                = "relay.tunnel.cloudproxy.app"
	SUBPROTOCOL_TAG_LEN             = 2
	SUBPROTOCOL_HEADER_LEN          = SUBPROTOCOL_TAG_LEN + 4 // 6 bytes
	SUBPROTOCOL_MAX_DATA_FRAME_SIZE = 16384

	SUBPROTOCOL_TAG_CONNECT_SUCCESS_SID   uint16 = 0x0001
	SUBPROTOCOL_TAG_RECONNECT_SUCCESS_ACK uint16 = 0x0002
	SUBPROTOCOL_TAG_DATA                  uint16 = 0x0004
	SUBPROTOCOL_TAG_ACK                   uint16 = 0x0007
)

// TunnelTarget describes the remote IAP tunnel endpoint.
type TunnelTarget struct {
	Project     string
	Zone        string
	Instance    string
	Interface   string
	Port        int
	URLOverride string
	Network     string
	Region      string
	Host        string
	DestGroup   string
}

// TunnelAdapter implements io.ReadWriteCloser for the tunnel.
type TunnelAdapter struct {
	mu              sync.Mutex
	conn            *websocket.Conn
	protocol        TunnelProtocol
	target          TunnelTarget
	inbound         chan []byte
	totalInboundLen uint64
	pending         []byte
	errors          chan error
	closed          chan struct{}
	ready           chan struct{}
	sid             uint64
	lastAckBytes    uint64
}

// NewTunnelAdapter creates a TunnelAdapter that implements io.ReadWriteCloser over the IAP websocket connection.
// protocol should implement your IAP tunnel protocol logic.
func NewTunnelAdapter(wsConn *websocket.Conn, target TunnelTarget) *TunnelAdapter {
	return &TunnelAdapter{
		conn:     wsConn,
		protocol: NewIAPTunnelProtocol(),
		target:   target,
		inbound:  make(chan []byte, 100),
		errors:   make(chan error, 100),
		closed:   make(chan struct{}),
		ready:    make(chan struct{}),
	}
}

func (t *TunnelAdapter) Read(p []byte) (int, error) {
	// Serve any pending data first
	if len(t.pending) > 0 {
		n := copy(p, t.pending)
		t.pending = t.pending[n:]
		return n, nil
	}

	select {
	case data, ok := <-t.inbound:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, data)
		// Save any leftover bytes for the next Read
		if n < len(data) {
			t.pending = data[n:]
		}
		return n, nil
	case <-t.closed:
		return 0, io.EOF
	}
}

func (t *TunnelAdapter) Write(p []byte) (int, error) {
	return t.protocol.SendDataFrame(t.conn, p)
}

func (t *TunnelAdapter) Close() error {
	close(t.closed)
	return t.conn.Close(websocket.StatusNormalClosure, "tunnel closed")
}

// Start launches goroutines to handle inbound websocket messages and protocol parsing.
func (t *TunnelAdapter) Start(ctx context.Context) {
	go func() {
		defer close(t.inbound)
		defer close(t.errors)

		// var err error
		for {
			_, msg, err := t.conn.Read(ctx)
			if err != nil {
				// Attempt reconnect if not context cancellation
				if ctx.Err() == nil && t.sid != 0 {
					reconnectURL, _ := CreateWebSocketReconnectURL(
						t.target, t.sid, t.lastAckBytes, true,
					)
					wsConn, _, err := websocket.Dial(ctx, reconnectURL, nil)
					if err != nil {
						t.errors <- fmt.Errorf("reconnect failed: %w", err)
						logger.Error("Reconnect failed", "err", err)
						return
					}
					t.mu.Lock()
					t.conn = wsConn
					t.mu.Unlock()
					continue
				}
				t.errors <- err
				logger.Error("Websocket error", "err", err)
				return
			}
			// Parse protocol and push data frames to t.inbound
			frameType, parsedMsg, err := t.protocol.ParseFrame(msg)
			if err != nil {
				t.errors <- err
				logger.Error("Protocol parse error", "err", err)
				return
			}
			t.handleFrame(frameType, parsedMsg)
		}
	}()
}

func (t *TunnelAdapter) handleFrame(frameType uint16, parsedMsg []byte) {
	switch frameType {
	case SUBPROTOCOL_TAG_CONNECT_SUCCESS_SID:
		t.handleConnectSuccess(parsedMsg)
	case SUBPROTOCOL_TAG_RECONNECT_SUCCESS_ACK:
		t.handleReconnectSuccessAck(parsedMsg)
	case SUBPROTOCOL_TAG_ACK:
		t.handleAck(parsedMsg)
	case SUBPROTOCOL_TAG_DATA:
		t.handleData(parsedMsg)
	default:
		logger.Warn("Unknown frame type", "frameType", frameType)
	}
}

// handleConnectSuccess processes the CONNECT_SUCCESS_SID frame and extracts the SID.
// This is used to identify the tunnel session.
// It also signals that the tunnel is ready for use.
func (t *TunnelAdapter) handleConnectSuccess(msg []byte) {
	logger.Debug("Received Connect Success SID frame")
	sid, _, err := t.protocol.ExtractSubprotocolConnectSuccessSid(msg)
	if err != nil {
		t.errors <- fmt.Errorf("unable to extract connect success SID: %w", err)
		logger.Error("Unable to extract connect success SID", "err", err)
	}
	t.mu.Lock()
	t.sid = sid
	t.mu.Unlock()
	select {
	case <-t.ready:
		// already closed
	default:
		close(t.ready)
	}
}

// handleReconnectSuccessAck processes the RECONNECT_SUCCESS_ACK frame and extracts the ACK bytes.
// This is used to acknowledge the successful reconnection of the tunnel.
func (t *TunnelAdapter) handleReconnectSuccessAck(msg []byte) {
	logger.Debug("Received RECONNECT_SUCCESS_ACK frame")
	ackBytes, _, err := t.protocol.ExtractSubprotocolAck(msg)
	if err != nil {
		t.errors <- fmt.Errorf("unable to extract ack from reconnect success: %w", err)
		logger.Error("Unable to extract ack from reconnect success", "err", err)
	}
	t.mu.Lock()
	t.lastAckBytes = ackBytes
	t.mu.Unlock()
}

// handleAck processes the ACK frame and updates the last acknowledged bytes.
// This is used to track the last acknowledged data frame.
// It is important for flow control and ensuring that the sender does not overwhelm the receiver.
// It also helps in managing the state of the tunnel connection.
func (t *TunnelAdapter) handleAck(msg []byte) {
	logger.Debug("Received ACK frame")
	ackBytes, _, err := t.protocol.ExtractSubprotocolAck(msg)
	if err != nil {
		t.errors <- fmt.Errorf("unable to extract ack: %w", err)
		logger.Error("Unable to extract ack", "err", err)
	}
	t.mu.Lock()
	t.lastAckBytes = ackBytes
	t.mu.Unlock()
}

// handleData processes the DATA frame and extracts the data.
func (t *TunnelAdapter) handleData(msg []byte) {
	logger.Debug("Received data frame")
	data, _, err := t.protocol.ExtractSubprotocolData(msg)
	if err != nil {
		t.errors <- fmt.Errorf("unable to extract data: %w", err)
		logger.Error("Unable to extract data", "err", err)
	}
	if data != nil {
		t.inbound <- data
		t.totalInboundLen += uint64(len(data))
		if t.totalInboundLen > 2*SUBPROTOCOL_MAX_DATA_FRAME_SIZE {
			_, err := t.protocol.SendAckFrame(t.conn, t.totalInboundLen)
			if err != nil {
				t.errors <- err
				logger.Error("Failed to send ACK frame", "err", err)
			}
			t.totalInboundLen = 0
		}
	}
}

// Ready returns a channel that signals when the tunnel is ready.
func (t *TunnelAdapter) Ready() <-chan struct{} {
	return t.ready
}

// TunnelManager manages the lifecycle of a local TCP listener and IAP tunnel connections.
type TunnelManager struct {
	target TunnelTarget
	auth   TokenProvider

	lastTunnel *TunnelAdapter
	mu         sync.Mutex
	errors     chan error
	running    bool
}

// NewTunnelManager creates a new TunnelManager.
func NewTunnelManager(target TunnelTarget, auth TokenProvider) *TunnelManager {
	return &TunnelManager{
		target: target,
		auth:   auth,
		errors: make(chan error, 100),
	}
}

// Errors returns a channel for asynchronous error reporting.
func (m *TunnelManager) Errors() <-chan error {
	return m.errors
}

// Serve starts accepting connections on the provided listener and proxies them via IAP.
func (m *TunnelManager) Serve(ctx context.Context, lis net.Listener) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return errors.New("tunnel manager already running")
	}
	m.running = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()

	logger.Info("IAP Tunnel Serving", "addr", lis.Addr())
	defer func() {
		logger.Info("IAP Tunnel Listener closed, shutting down Serve loop")
	}()

	for {
		conn, err := lis.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
				return nil
			}
			select {
			case m.errors <- fmt.Errorf("accept error: %w", err):
			default:
			}
			continue
		}
		go m.handleConn(ctx, conn)
	}
}

// handleConn proxies a single TCP connection via IAP WebSocket.
func (m *TunnelManager) handleConn(ctx context.Context, conn net.Conn) {
	logger.DebugContext(ctx, "Accepted connection", "remoteAddr", conn.RemoteAddr())

	wsConn, _, err := m.startWebSocket(ctx)
	if err != nil {
		logger.Error("Failed to start websocket", "err", err)
		return
	}

	tunnel := NewTunnelAdapter(wsConn, m.target)
	m.mu.Lock()
	m.lastTunnel = tunnel
	m.mu.Unlock()
	tunnel.Start(ctx)
	defer func() {
		if err := conn.Close(); err != nil {
			logger.Error("Failed to close connection", "err", err)
		}
		if err := wsConn.Close(websocket.StatusNormalClosure, "handler done"); err != nil {
			logger.Error("Failed to close websocket", "err", err)
		}
		if err := tunnel.Close(); err != nil {
			logger.Error("Failed to close tunnel", "err", err)
		}
	}()
	go m.forwardErrors(tunnel)

	// Wait for CONNECT_SUCCESS before proxying
	select {
	case <-tunnel.ready:
		// Tunnel is ready
	case <-ctx.Done():
		return
	}

	m.setTCPKeepAlive(conn)

	err = proxyBidirectional(ctx, conn, tunnel)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		logger.Error("Proxy error", "err", err)
	}
	logger.DebugContext(ctx, "Closed connection", "remoteAddr", conn.RemoteAddr())
}

func (m *TunnelManager) forwardErrors(tunnel *TunnelAdapter) {
	for err := range tunnel.errors {
		select {
		case m.errors <- err:
		default:
		}
	}
}

func (m *TunnelManager) setTCPKeepAlive(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.SetKeepAlive(true); err != nil {
			logger.Error("Failed to set keepalive", "err", err)
		}
		if err := tcpConn.SetKeepAlivePeriod(30 * time.Second); err != nil {
			logger.Error("Failed to set keepalive period", "err", err)
		}
	}
}

func (m *TunnelManager) Ready() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastTunnel != nil {
		return m.lastTunnel.Ready()
	}
	// Return a closed channel if not available
	ch := make(chan struct{})
	close(ch)
	return ch
}

// startWebSocket establishes a websocket connection to the IAP tunnel backend.
func (m *TunnelManager) startWebSocket(ctx context.Context) (*websocket.Conn, *http.Response, error) {
	if m.auth == nil {
		src, err := google.DefaultTokenSource(ctx, oauthsvc.UserinfoEmailScope)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to acquire token source: %w", err)
		}
		m.auth = NewOAuthTokenProvider(src)
	}
	u, err := CreateWebSocketConnectURL(m.target, true)
	if err != nil {
		return nil, nil, err
	}
	headers, err := m.auth.GetHeaders()
	if err != nil {
		return nil, nil, err
	}
	opts := &websocket.DialOptions{
		HTTPHeader:   headers,
		Subprotocols: []string{SUBPROTOCOL_NAME},
	}
	return websocket.Dial(ctx, u, opts)
}

// proxyBidirectional copies data in both directions until either side closes.
func proxyBidirectional(ctx context.Context, a, b io.ReadWriteCloser) error {
	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error { _, err := io.Copy(a, b); return err })
	g.Go(func() error { _, err := io.Copy(b, a); return err })
	return g.Wait()
}

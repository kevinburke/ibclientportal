package ibclientportal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Market data field codes used by the streaming websocket (and the REST
// /iserver/marketdata/snapshot endpoint). These are passed in the fields
// argument to (*Stream).SubscribeMarketData and appear as keys in the
// MarketDataUpdate.Fields map. This is a commonly-used subset; any numeric
// field code documented by IBKR may be requested as a raw string.
//
// https://www.interactivebrokers.com/campus/ibkr-api-page/cpapi-v1/#market-data-fields
const (
	FieldLastPrice     = "31"
	FieldSymbol        = "55"
	FieldText          = "58"
	FieldHigh          = "70"
	FieldLow           = "71"
	FieldMarketValue   = "73"
	FieldAvgPrice      = "74"
	FieldUnrealizedPnL = "75"
	FieldChange        = "82"
	FieldChangePercent = "83"
	FieldBidPrice      = "84"
	FieldAskSize       = "85"
	FieldAskPrice      = "86"
	FieldVolume        = "87"
	FieldBidSize       = "88"
	FieldExchange      = "6004"
	FieldConid         = "6008"
	FieldCompanyName   = "7051"
)

const (
	// defaultHeartbeatInterval is how often the Stream sends a "tic" keep-alive
	// to the gateway. IBKR recommends periodic keep-alives to avoid the
	// connection being dropped.
	defaultHeartbeatInterval = 30 * time.Second

	// reconnect backoff bounds and the per-attempt connect timeout used by the
	// supervisor when the connection drops.
	reconnectMinBackoff = 500 * time.Millisecond
	reconnectMaxBackoff = 30 * time.Second
	connectTimeout      = 45 * time.Second
)

// wsDebugf writes a diagnostic line to stderr when IBCP_WS_DEBUG is set. It is
// the single debug hook for the streaming code; it is silent by default.
func wsDebugf(format string, args ...any) {
	if os.Getenv("IBCP_WS_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "WSDEBUG "+format+"\n", args...)
	}
}

// MarketDataUpdate is a single streaming market-data message for one contract,
// delivered on (*Stream).Updates. The gateway sends a message whenever one or
// more subscribed fields change; only the changed fields are present, so
// callers should treat updates as incremental and maintain their own latest
// state per conid.
type MarketDataUpdate struct {
	// Conid is the contract identifier this update is for.
	Conid int
	// Topic is the raw websocket topic, e.g. "smd+265598".
	Topic string
	// Fields maps IB numeric field codes (see the Field* constants) to their
	// raw JSON values. Values may be JSON strings or numbers depending on the
	// field; use String or Float to read them without caring which.
	Fields map[string]json.RawMessage
}

// String returns the value of the given field code as a string, stripping
// surrounding quotes if the underlying JSON value was a string. The second
// return value reports whether the field was present in this update.
func (u MarketDataUpdate) String(field string) (string, bool) {
	raw, ok := u.Fields[field]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	return string(raw), true
}

// Float returns the value of the given field code as a float64. It handles
// both JSON-number and JSON-string encodings. The second return value reports
// whether the field was present and parseable as a number.
func (u MarketDataUpdate) Float(field string) (float64, bool) {
	s, ok := u.String(field)
	if !ok {
		return 0, false
	}
	// IB sometimes prefixes prices with markers such as 'C' (previous close)
	// or 'H'/'L'; strip any leading non-numeric marker characters.
	s = strings.TrimLeft(s, "CHBAlch ")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// Stream is a live websocket connection to the Client Portal Gateway's
// streaming endpoint (wss://<host>/v1/api/ws). It multiplexes subscriptions
// over a single connection and reconnects automatically if the connection
// drops, replaying all active subscriptions on the new connection. A Stream is
// safe for concurrent use by multiple goroutines.
//
// Obtain a Stream with (*Client).DialStream. Always call Close when done.
type Stream struct {
	client  *Client
	ctx     context.Context // governs the stream lifetime, including reconnects
	updates chan MarketDataUpdate

	connMu sync.Mutex
	conn   *websocket.Conn

	writeMu sync.Mutex // serializes writes to the current connection

	subsMu sync.Mutex
	subs   map[int][]string // conid -> requested fields, replayed on reconnect

	closeOnce sync.Once
	done      chan struct{}

	errMu sync.Mutex
	err   error
}

// wsURL derives the streaming websocket URL from the client's host, converting
// the http(s) scheme to ws(s).
func (c *Client) wsURL() string {
	u := c.host + "/v1/api/ws"
	if strings.HasPrefix(u, "https://") {
		return "wss://" + strings.TrimPrefix(u, "https://")
	}
	if strings.HasPrefix(u, "http://") {
		return "ws://" + strings.TrimPrefix(u, "http://")
	}
	return u
}

// DialStream opens a streaming websocket connection to the Client Portal
// Gateway. The gateway authenticates the socket from the brokerage session
// itself (via the shared cookie jar on the localhost gateway), so the client
// must already hold an authenticated session, as it must for the REST
// market-data endpoints.
//
// The first connection is established synchronously: DialStream calls Tickle
// and fails fast with a clear error if the session is not authenticated (rather
// than opening a socket that would then silently deliver no data), and it waits
// for the gateway's session-established frame before returning so that a
// subscription is never sent too early. It does not send a session token over
// the socket: unlike the hosted OAuth endpoint, the gateway rejects such a
// message as an unknown topic.
//
// After the first connection, the Stream reconnects automatically if the
// connection drops, replaying every active subscription on the new connection.
// The Updates channel stays open across reconnects and is closed only when the
// stream ends: either Close is called or ctx is cancelled. Because ctx governs
// reconnect attempts too, pass a context that lives as long as you want the
// stream (for example one you cancel at shutdown), not a short per-request
// context.
//
// If SetInsecureSkipVerify was called on the client (typical for the default
// self-signed localhost gateway), the websocket dial reuses that TLS setting.
func (c *Client) DialStream(ctx context.Context) (*Stream, error) {
	s := &Stream{
		client:  c,
		ctx:     ctx,
		updates: make(chan MarketDataUpdate, 256),
		subs:    make(map[int][]string),
		done:    make(chan struct{}),
	}
	conn, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	s.setConn(conn)

	go s.supervise()
	go s.heartbeatLoop()
	return s, nil
}

// connect performs one full connection attempt: it verifies the session via
// Tickle, dials the websocket, and waits for the session-established frame.
func (s *Stream) connect(ctx context.Context) (*websocket.Conn, error) {
	c := s.client
	tickle, err := c.Tickle(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("ibclientportal: streaming: checking session via tickle: %w", err)
	}
	if !tickle.IServer.AuthStatus.Authenticated {
		return nil, fmt.Errorf("ibclientportal: streaming: brokerage session is not authenticated (tickle: %q); log in to the gateway first", tickle.IServer.AuthStatus.Message)
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: 45 * time.Second,
		// Reuse the client's session cookies for authentication.
		Jar: c.Client.Client.Jar,
	}
	if tr, ok := c.httpTransport(); ok && tr.TLSClientConfig != nil {
		tlsConf := tr.TLSClientConfig.Clone()
		// The REST transport enables HTTP/2, so its TLS config advertises
		// "h2" via ALPN. A websocket upgrade must run over HTTP/1.1, so clear
		// the negotiated protocols; otherwise the server may pick h2 and the
		// handshake fails ("protocol \"h2\" was given but is not supported").
		tlsConf.NextProtos = nil
		dialer.TLSClientConfig = tlsConf
	}

	header := http.Header{}
	header.Set("User-Agent", UserAgent)

	conn, resp, err := dialer.DialContext(ctx, c.wsURL(), header)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("ibclientportal: streaming: dialing %s: %w (HTTP %d)", c.wsURL(), err, resp.StatusCode)
		}
		return nil, fmt.Errorf("ibclientportal: streaming: dialing %s: %w", c.wsURL(), err)
	}

	// The gateway silently ignores subscriptions sent before it reports the
	// session status. Wait for the "sts" frame (session established) so callers
	// (and the resubscribe path) cannot subscribe too early and get no data.
	if err := awaitEstablished(ctx, conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// awaitEstablished reads the gateway's initial handshake frames until it
// reports session status via the "sts" topic. On connect the gateway emits a
// short burst of non-market-data frames ("system", "act", "sts"); this drains
// them (losing no market data, since nothing is subscribed yet) and confirms
// the session is authenticated before any subscription is sent.
func awaitEstablished(ctx context.Context, conn *websocket.Conn) error {
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return fmt.Errorf("ibclientportal: streaming: %w", err)
	}
	defer conn.SetReadDeadline(time.Time{})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("ibclientportal: streaming: waiting for session status: %w", err)
		}
		wsDebugf("handshake: %s", data)
		var frame struct {
			Topic string `json:"topic"`
			Args  struct {
				Authenticated bool   `json:"authenticated"`
				Message       string `json:"message"`
			} `json:"args"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		if frame.Topic != "sts" {
			continue
		}
		if !frame.Args.Authenticated {
			return fmt.Errorf("ibclientportal: streaming: gateway reports session not authenticated: %q", frame.Args.Message)
		}
		return nil
	}
}

// supervise reads the current connection until it fails, then reconnects with
// capped exponential backoff and replays every active subscription. It runs for
// the life of the Stream and closes the Updates channel when the stream ends.
func (s *Stream) supervise() {
	defer close(s.updates)

	backoff := reconnectMinBackoff
	for {
		conn := s.currentConn()
		s.readConn(conn)
		conn.Close() // the connection has failed (or Close was called); free it.

		select {
		case <-s.done:
			return
		default:
		}
		if err := s.ctx.Err(); err != nil {
			s.setErr(err)
			return
		}

		wsDebugf("connection lost; reconnecting")
		for {
			if !s.wait(backoff) {
				if err := s.ctx.Err(); err != nil {
					s.setErr(err)
				}
				return
			}
			backoff = min(backoff*2, reconnectMaxBackoff)

			attemptCtx, cancel := context.WithTimeout(s.ctx, connectTimeout)
			newConn, err := s.connect(attemptCtx)
			cancel()
			if err != nil {
				wsDebugf("reconnect failed: %v", err)
				continue
			}
			s.setConn(newConn)
			s.resubscribeAll()
			backoff = reconnectMinBackoff
			wsDebugf("reconnected")
			break
		}
	}
}

// readConn reads and dispatches frames from conn until it returns an error
// (connection closed or failed).
func (s *Stream) readConn(conn *websocket.Conn) {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		wsDebugf("recv: %s", data)
		s.dispatch(data)
	}
}

// wait sleeps for d, returning true if it elapsed or false if the stream ended
// (Close called or ctx cancelled) first.
func (s *Stream) wait(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-s.done:
		return false
	case <-s.ctx.Done():
		return false
	}
}

func (s *Stream) setConn(conn *websocket.Conn) {
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
}

func (s *Stream) currentConn() *websocket.Conn {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn
}

// SubscribeMarketData begins streaming market data for the given conid. Updates
// are delivered on the Updates channel. If fields is empty, a default set
// (last, bid, ask, and their sizes) is requested.
//
// The subscription is recorded and automatically re-sent whenever the stream
// reconnects, so it persists for the life of the Stream until
// UnsubscribeMarketData is called. Calling it again for the same conid replaces
// the field set. A momentary send failure (for example during a reconnect) is
// not returned as an error, because the subscription is replayed on reconnect.
//
// The REST prerequisites for market data apply: an authenticated session and a
// prior call to /iserver/accounts. For derivative contracts, /iserver/secdef
// search must have been called first. The first update for a contract may be a
// partial snapshot; subsequent updates are incremental.
func (s *Stream) SubscribeMarketData(conid int, fields ...string) error {
	if len(fields) == 0 {
		fields = []string{
			FieldLastPrice, FieldBidPrice, FieldAskPrice,
			FieldBidSize, FieldAskSize, FieldVolume,
		}
	}
	s.subsMu.Lock()
	s.subs[conid] = fields
	s.subsMu.Unlock()

	if err := s.sendSubscribe(conid, fields); err != nil {
		wsDebugf("subscribe conid %d send failed (will retry on reconnect): %v", conid, err)
	}
	return nil
}

// UnsubscribeMarketData stops streaming market data for the given conid and
// removes it from the set replayed on reconnect.
func (s *Stream) UnsubscribeMarketData(conid int) error {
	s.subsMu.Lock()
	delete(s.subs, conid)
	s.subsMu.Unlock()
	return s.writeText(fmt.Sprintf("umd+%d+{}", conid))
}

func (s *Stream) sendSubscribe(conid int, fields []string) error {
	args, err := json.Marshal(struct {
		Fields []string `json:"fields"`
	}{Fields: fields})
	if err != nil {
		return err
	}
	return s.writeText(fmt.Sprintf("smd+%d+%s", conid, args))
}

// resubscribeAll replays every recorded subscription on the current connection.
// Called by the supervisor after a successful reconnect.
func (s *Stream) resubscribeAll() {
	s.subsMu.Lock()
	subs := make(map[int][]string, len(s.subs))
	for conid, fields := range s.subs {
		subs[conid] = fields
	}
	s.subsMu.Unlock()

	for conid, fields := range subs {
		if err := s.sendSubscribe(conid, fields); err != nil {
			wsDebugf("resubscribe conid %d failed: %v", conid, err)
		}
	}
}

// Updates returns the channel on which market-data updates are delivered. The
// channel stays open across automatic reconnects and is closed only when the
// stream ends (Close is called or the dial context is cancelled); check Err
// afterwards to distinguish a clean Close from a context error.
func (s *Stream) Updates() <-chan MarketDataUpdate {
	return s.updates
}

// Err returns the error that ended the Stream, or nil if it was closed cleanly
// via Close. It is only meaningful after the Updates channel is closed.
// Transient connection drops are handled by reconnecting and are not reported
// here.
func (s *Stream) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

// Close shuts down the stream and its underlying connection, ending the
// reconnect loop. It is safe to call multiple times.
func (s *Stream) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
	})
	if conn := s.currentConn(); conn != nil {
		return conn.Close()
	}
	return nil
}

func (s *Stream) writeText(msg string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	conn := s.currentConn()
	if conn == nil {
		return fmt.Errorf("ibclientportal: streaming: not connected")
	}
	return conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// setErr records the error that ended the stream (only the first is kept).
func (s *Stream) setErr(err error) {
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
}

func (s *Stream) heartbeatLoop() {
	t := time.NewTicker(defaultHeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-s.ctx.Done():
			return
		case <-t.C:
			// Best effort: if disconnected, the supervisor is reconnecting.
			if err := s.writeText("tic"); err != nil {
				wsDebugf("heartbeat write failed: %v", err)
			}
		}
	}
}

// dispatch parses a raw websocket frame and, if it is a market-data update,
// delivers it on the updates channel. Non-market-data frames (system messages,
// heartbeats, other topics) are ignored in this SMD-focused implementation.
func (s *Stream) dispatch(data []byte) {
	var envelope struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return
	}
	if !strings.HasPrefix(envelope.Topic, "smd+") {
		return
	}
	conid, err := strconv.Atoi(strings.TrimPrefix(envelope.Topic, "smd+"))
	if err != nil {
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return
	}
	// Drop the envelope/bookkeeping keys, leaving only field codes.
	delete(fields, "topic")
	delete(fields, "conid")
	delete(fields, "conidEx")
	delete(fields, "server_id")
	delete(fields, "_updated")

	update := MarketDataUpdate{Conid: conid, Topic: envelope.Topic, Fields: fields}
	select {
	case s.updates <- update:
	case <-s.done:
	}
}

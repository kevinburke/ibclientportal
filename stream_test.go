package ibclientportal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// writeTickleAuthed responds to /v1/api/tickle with an authenticated session,
// matching the shape (*Client).DialStream checks.
func writeTickleAuthed(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"session": "sess",
		"iserver": map[string]any{
			"authStatus": map[string]any{"authenticated": true},
		},
	})
}

// stsFrame is the session-established frame the gateway emits on connect,
// before it will honor subscriptions.
const stsFrame = `{"topic":"sts","args":{"authenticated":true,"connected":true}}`

func TestWSURL(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"https://localhost:5000": "wss://localhost:5000/v1/api/ws",
		"http://127.0.0.1:8080":  "ws://127.0.0.1:8080/v1/api/ws",
	}
	for host, want := range cases {
		c := &Client{host: host}
		if got := c.wsURL(); got != want {
			t.Errorf("wsURL(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestMarketDataUpdateAccessors(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"topic":"smd+265598","conid":265598,"31":"420.50","84":420.49,"55":"AAPL","86":"C419.00"}`)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	u := MarketDataUpdate{Conid: 265598, Fields: fields}

	if got, ok := u.String(FieldSymbol); !ok || got != "AAPL" {
		t.Errorf("String(symbol) = %q, %v; want AAPL, true", got, ok)
	}
	// JSON-string encoded number.
	if got, ok := u.Float(FieldLastPrice); !ok || got != 420.50 {
		t.Errorf("Float(last) = %v, %v; want 420.50, true", got, ok)
	}
	// JSON-number encoded value.
	if got, ok := u.Float(FieldBidPrice); !ok || got != 420.49 {
		t.Errorf("Float(bid) = %v, %v; want 420.49, true", got, ok)
	}
	// Value with a leading marker character ('C' = previous close).
	if got, ok := u.Float(FieldAskPrice); !ok || got != 419.00 {
		t.Errorf("Float(ask) = %v, %v; want 419.00, true", got, ok)
	}
	// Missing field.
	if _, ok := u.Float("99999"); ok {
		t.Error("Float(missing) reported present")
	}
}

// TestStreamRoundTrip spins up a fake gateway that speaks the tickle + websocket
// protocol and verifies the full DialStream -> Subscribe -> Updates path,
// including the session-confirmation handshake and the smd subscribe framing.
func TestStreamRoundTrip(t *testing.T) {
	t.Parallel()

	const conid = 265598

	gotSubscribe := make(chan string, 1)

	var upgrader websocket.Upgrader
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/api/tickle", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("tickle: method = %s, want POST", r.Method)
		}
		writeTickleAuthed(w)
	})
	mux.HandleFunc("/v1/api/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		// Gateway emits a session-status frame once the stream is established.
		conn.WriteMessage(websocket.TextMessage, []byte(stsFrame))
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg := string(data)
			switch {
			case strings.HasPrefix(msg, "smd+"):
				gotSubscribe <- msg
				// Reply with a market-data frame.
				conn.WriteMessage(websocket.TextMessage,
					[]byte(`{"topic":"smd+265598","conid":265598,"server_id":"q0","31":"420.50","84":"420.49"}`))
			case msg == "tic":
				// keep-alive; ignore
			default:
				t.Errorf("gateway received unexpected frame: %q", msg)
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := New(srv.URL)
	stream, err := client.DialStream(ctx)
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer stream.Close()

	if err := stream.SubscribeMarketData(conid, FieldLastPrice, FieldBidPrice); err != nil {
		t.Fatalf("SubscribeMarketData: %v", err)
	}
	select {
	case sub := <-gotSubscribe:
		want := `smd+265598+{"fields":["31","84"]}`
		if sub != want {
			t.Errorf("subscribe frame = %q, want %q", sub, want)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for subscribe frame")
	}

	select {
	case u := <-stream.Updates():
		if u.Conid != conid {
			t.Errorf("update conid = %d, want %d", u.Conid, conid)
		}
		if got, ok := u.Float(FieldLastPrice); !ok || got != 420.50 {
			t.Errorf("update last price = %v, %v; want 420.50, true", got, ok)
		}
		// Bookkeeping keys must be stripped from Fields.
		if _, ok := u.Fields["server_id"]; ok {
			t.Error("server_id leaked into Fields")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for market-data update")
	}
}

// TestStreamReconnect verifies that when the gateway drops the connection, the
// Stream reconnects, replays the active subscription, and keeps delivering on
// the same Updates channel without the caller re-subscribing.
func TestStreamReconnect(t *testing.T) {
	t.Parallel()

	const conid = 8314

	var conns int32
	subscribed := make(chan int32, 4) // connection index that received a subscribe

	var upgrader websocket.Upgrader
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/api/tickle", func(w http.ResponseWriter, r *http.Request) {
		writeTickleAuthed(w)
	})
	mux.HandleFunc("/v1/api/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		n := atomic.AddInt32(&conns, 1)
		conn.WriteMessage(websocket.TextMessage, []byte(stsFrame))
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg := string(data)
			switch {
			case strings.HasPrefix(msg, "smd+"):
				subscribed <- n
				if n == 1 {
					// First connection: drop immediately after the first
					// subscribe to force a reconnect.
					return
				}
				// Later connections: deliver a market-data frame.
				conn.WriteMessage(websocket.TextMessage,
					[]byte(`{"topic":"smd+8314","conid":8314,"31":"12.34"}`))
			case msg == "tic":
				// keep-alive; ignore
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := New(srv.URL)
	stream, err := client.DialStream(ctx)
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer stream.Close()

	// Subscribe once; the caller never subscribes again after the drop.
	if err := stream.SubscribeMarketData(conid, FieldLastPrice); err != nil {
		t.Fatalf("SubscribeMarketData: %v", err)
	}

	// First subscribe lands on connection 1, which then drops.
	select {
	case n := <-subscribed:
		if n != 1 {
			t.Fatalf("first subscribe arrived on connection %d, want 1", n)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for first subscribe")
	}

	// The Stream must reconnect and replay the subscription itself.
	select {
	case n := <-subscribed:
		if n < 2 {
			t.Fatalf("replayed subscribe arrived on connection %d, want >= 2", n)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for replayed subscribe after reconnect")
	}

	// And the update delivered on the reconnected socket reaches the caller on
	// the same Updates channel.
	select {
	case u := <-stream.Updates():
		if u.Conid != conid {
			t.Errorf("update conid = %d, want %d", u.Conid, conid)
		}
		if got, ok := u.Float(FieldLastPrice); !ok || got != 12.34 {
			t.Errorf("update last price = %v, %v; want 12.34, true", got, ok)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for post-reconnect update")
	}

	if got := atomic.LoadInt32(&conns); got < 2 {
		t.Errorf("gateway saw %d connections, want >= 2 (reconnect)", got)
	}
}

// TestStreamRenewResubscribes verifies that a renewal re-sends every active
// subscription on the live connection, without a reconnect, so subscriptions
// outlive the gateway's 10-minute expiry. It drives the mechanism the renewal
// timer invokes on each tick directly, rather than waiting on the wall-clock
// interval.
func TestStreamRenewResubscribes(t *testing.T) {
	t.Parallel()

	subscribes := make(chan int, 8) // conids the gateway received smd requests for

	var upgrader websocket.Upgrader
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/api/tickle", func(w http.ResponseWriter, r *http.Request) {
		writeTickleAuthed(w)
	})
	mux.HandleFunc("/v1/api/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		conn.WriteMessage(websocket.TextMessage, []byte(stsFrame))
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			msg := string(data)
			if !strings.HasPrefix(msg, "smd+") {
				continue // ignore keep-alives
			}
			// Parse the conid out of "smd+<conid>+{...}".
			rest := strings.TrimPrefix(msg, "smd+")
			plus := strings.IndexByte(rest, '+')
			if plus < 0 {
				t.Errorf("malformed smd frame: %q", msg)
				continue
			}
			conid, err := strconv.Atoi(rest[:plus])
			if err != nil {
				t.Errorf("bad conid in %q: %v", msg, err)
				continue
			}
			subscribes <- conid
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := New(srv.URL)
	stream, err := client.DialStream(ctx)
	if err != nil {
		t.Fatalf("DialStream: %v", err)
	}
	defer stream.Close()

	want := []int{111, 222}
	for _, conid := range want {
		if err := stream.SubscribeMarketData(conid, FieldLastPrice); err != nil {
			t.Fatalf("SubscribeMarketData(%d): %v", conid, err)
		}
	}

	// Collect the initial subscribes so the renewal round is unambiguous.
	drainSubscribes(t, ctx, subscribes, want)

	// A renewal re-sends every active subscription on the same connection.
	stream.resubscribeAll()
	drainSubscribes(t, ctx, subscribes, want)
}

// drainSubscribes reads len(want) conids from ch and asserts they are exactly
// the set want (order-independent, since subscriptions are stored in a map).
func drainSubscribes(t *testing.T, ctx context.Context, ch <-chan int, want []int) {
	t.Helper()
	got := make(map[int]bool)
	for range want {
		select {
		case conid := <-ch:
			got[conid] = true
		case <-ctx.Done():
			t.Fatalf("timed out waiting for subscribes; got %v, want %v", got, want)
		}
	}
	for _, conid := range want {
		if !got[conid] {
			t.Errorf("missing subscribe for conid %d; got %v", conid, got)
		}
	}
}

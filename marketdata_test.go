package ibclientportal

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSnapshot(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		infoCh <- requestInfo{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}
		w.Header().Set("Content-Type", "application/json")
		// Field values arrive as strings, and IB prefixes some prices with a
		// marker letter ('C' = previous close on a contract that has not
		// traded yet). A conid the backend has not resolved yet comes back
		// with no price fields at all.
		_, _ = w.Write([]byte(`[` +
			`{"conid":265598,"conidEx":"265598","server_id":"q0","_updated":1234567890,` +
			`"31":"C123.45","84":"123.40","86":"123.50","88":"400","85":"300","6509":"RB"},` +
			`{"conid":8314,"server_id":"q1","_updated":1234567891}` +
			`]`))
	})
	defer server.Close()

	snapshots, err := client.MarketData.Snapshot(testContext(t), []int{265598, 8314},
		[]string{FieldLastPrice, FieldBidPrice, FieldAskPrice})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d: %#v", len(snapshots), snapshots)
	}

	first := snapshots[0]
	if first.Conid != 265598 {
		t.Errorf("unexpected conid %d", first.Conid)
	}
	if last, ok := first.Float(FieldLastPrice); !ok || last != 123.45 {
		t.Errorf("expected the 'C' marker to be stripped from the last price, got %v (ok=%t)", last, ok)
	}
	if bid, ok := first.Float(FieldBidPrice); !ok || bid != 123.40 {
		t.Errorf("unexpected bid %v (ok=%t)", bid, ok)
	}
	if ask, ok := first.Float(FieldAskPrice); !ok || ask != 123.50 {
		t.Errorf("unexpected ask %v (ok=%t)", ask, ok)
	}
	if s, ok := first.String("6509"); !ok || s != "RB" {
		t.Errorf("unexpected availability field %q (ok=%t)", s, ok)
	}
	// Bookkeeping keys are not market-data fields and must not be exposed as
	// such, or a caller iterating the fields would treat them as quotes.
	for _, key := range []string{"conid", "conidEx", "server_id", "_updated"} {
		if _, ok := first.Fields[key]; ok {
			t.Errorf("expected %q to be stripped from the fields map", key)
		}
	}

	// A row with no price yet must report "absent", not zero: a zero price
	// would look like a real quote of $0.00.
	second := snapshots[1]
	if second.Conid != 8314 {
		t.Errorf("unexpected conid %d", second.Conid)
	}
	if price, ok := second.Float(FieldLastPrice); ok {
		t.Errorf("expected no last price for an unresolved contract, got %v", price)
	}

	info := <-infoCh
	if info.method != http.MethodGet {
		t.Errorf("expected GET, got %s", info.method)
	}
	if info.path != "/v1/api/iserver/marketdata/snapshot" {
		t.Errorf("unexpected path: %s", info.path)
	}
	query, err := url.ParseQuery(info.query)
	if err != nil {
		t.Fatalf("parsing query %q: %v", info.query, err)
	}
	if got := query.Get("conids"); got != "265598,8314" {
		t.Errorf("unexpected conids: %q", got)
	}
	if got := query.Get("fields"); got != "31,84,86" {
		t.Errorf("unexpected fields: %q", got)
	}
}

func TestSnapshotNoConids(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL.Path)
	})
	defer server.Close()

	if _, err := client.MarketData.Snapshot(testContext(t), nil, nil); err == nil {
		t.Error("expected an error when no conids are given")
	}
}

func TestMarketDataUnsubscribe(t *testing.T) {
	infoCh := make(chan requestInfo, 2)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		infoCh <- requestInfo{method: r.Method, path: r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"confirmed":true}`))
	})
	defer server.Close()

	ctx := testContext(t)
	if err := client.MarketData.Unsubscribe(ctx, 265598); err != nil {
		t.Fatalf("unsubscribe: %v", err)
	}
	if err := client.MarketData.UnsubscribeAll(ctx); err != nil {
		t.Fatalf("unsubscribe all: %v", err)
	}

	one := <-infoCh
	if one.method != http.MethodPost || one.path != "/v1/api/iserver/marketdata/unsubscribe" {
		t.Errorf("unexpected unsubscribe request: %s %s", one.method, one.path)
	}
	all := <-infoCh
	if all.method != http.MethodGet || all.path != "/v1/api/iserver/marketdata/unsubscribeall" {
		t.Errorf("unexpected unsubscribeall request: %s %s", all.method, all.path)
	}
}

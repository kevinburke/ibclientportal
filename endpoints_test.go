package ibclientportal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type requestInfo struct {
	method string
	path   string
	query  string
	cookie string
	body   string
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := New(server.URL)
	return client, server
}

func setStaleCookie(t *testing.T, c *Client, serverURL string) *url.URL {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	if c.Client.Client.Jar == nil {
		t.Fatal("expected cookie jar to be initialized")
	}
	c.Client.Client.Jar.SetCookies(u, []*http.Cookie{
		{Name: "x-sess-uuid", Value: "stale", Path: "/"},
	})
	if len(c.Client.Client.Jar.Cookies(u)) == 0 {
		t.Fatal("expected preexisting cookie in jar")
	}
	return u
}

func TestAuthStatusEndpoint(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		infoCh <- requestInfo{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			cookie: r.Header.Get("Cookie"),
			body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"authenticated":true,"competing":false,"connected":true,"message":"ok","MAC":"00:11:22","serverInfo":{"serverName":"test"},"hardware_info":"x86","fail":""}`))
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := client.AuthStatus(ctx)
	if err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if !resp.Authenticated || !resp.Connected || resp.Message != "ok" || resp.MAC != "00:11:22" {
		t.Fatalf("unexpected auth status response: %#v", resp)
	}
	if resp.ServerInfo["serverName"] != "test" || resp.HardwareInfo != "x86" {
		t.Fatalf("unexpected auth status server info: %#v", resp)
	}

	select {
	case info := <-infoCh:
		if info.method != http.MethodPost {
			t.Errorf("expected POST, got %s", info.method)
		}
		if info.path != "/v1/api/iserver/auth/status" {
			t.Errorf("unexpected path: %s", info.path)
		}
		if info.query != "" {
			t.Errorf("unexpected query: %q", info.query)
		}
		if info.cookie != "" {
			t.Errorf("unexpected cookie header: %q", info.cookie)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}
}

func TestListTradableAccountsEndpoint(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		infoCh <- requestInfo{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			cookie: r.Header.Get("Cookie"),
			body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accounts":["U12345"],"acctProps":{"type":"DEMO"},"aliases":{"U12345":"Primary"},"selectedAccount":"U12345","isFt":true,"isPaper":false}`))
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := client.Orders.ListTradableAccounts(ctx)
	if err != nil {
		t.Fatalf("list tradable accounts: %v", err)
	}
	if len(resp.Accounts) != 1 || resp.Accounts[0] != "U12345" {
		t.Fatalf("unexpected accounts response: %#v", resp)
	}
	if resp.SelectedAccount != "U12345" || !resp.IsFT || resp.IsPaper {
		t.Fatalf("unexpected selected account response: %#v", resp)
	}
	if resp.Aliases["U12345"] != "Primary" {
		t.Fatalf("unexpected aliases response: %#v", resp)
	}

	select {
	case info := <-infoCh:
		if info.method != http.MethodGet {
			t.Errorf("expected GET, got %s", info.method)
		}
		if info.path != "/v1/api/iserver/accounts" {
			t.Errorf("unexpected path: %s", info.path)
		}
		if info.query != "" {
			t.Errorf("unexpected query: %q", info.query)
		}
		if info.body != "" {
			t.Errorf("unexpected body: %q", info.body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}
}

func TestSwitchAccountClearsCookies(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		infoCh <- requestInfo{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			cookie: r.Header.Get("Cookie"),
			body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"set":true,"acctId":"U99999"}`))
	})
	defer server.Close()

	u := setStaleCookie(t, client, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := client.Orders.SwitchAccount(ctx, "U99999")
	if err != nil {
		t.Fatalf("switch account: %v", err)
	}
	if !resp.Set || resp.AccountID != "U99999" {
		t.Fatalf("unexpected switch account response: %#v", resp)
	}
	if got := client.Client.Client.Jar.Cookies(u); len(got) != 0 {
		t.Fatalf("expected cookie jar to be cleared, got %d cookies", len(got))
	}

	select {
	case info := <-infoCh:
		if info.method != http.MethodPost {
			t.Errorf("expected POST, got %s", info.method)
		}
		if info.path != "/v1/api/iserver/account" {
			t.Errorf("unexpected path: %s", info.path)
		}
		if info.cookie != "" {
			t.Errorf("expected no cookies, got %q", info.cookie)
		}
		var payload struct {
			AccountID string `json:"acctId"`
		}
		if err := json.Unmarshal([]byte(info.body), &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload.AccountID != "U99999" {
			t.Errorf("unexpected account id: %q", payload.AccountID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}
}

func TestListTransactionsEndpoint(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		infoCh <- requestInfo{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			cookie: r.Header.Get("Cookie"),
			body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(transactionsResponse)
	})
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := client.PerformanceAnalytics.ListTransactions(ctx, TransactionsRequest{
		AcctIDs:  []string{"U1234567"},
		Conids:   []int64{265598},
		Currency: "USD",
		Days:     30,
	})
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	if resp.Currency != "USD" {
		t.Errorf("expected Currency USD, got %s", resp.Currency)
	}
	if len(resp.Transactions) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(resp.Transactions))
	}

	select {
	case info := <-infoCh:
		if info.method != http.MethodPost {
			t.Errorf("expected POST, got %s", info.method)
		}
		if info.path != "/v1/api/pa/transactions" {
			t.Errorf("unexpected path: %s", info.path)
		}
		var payload TransactionsRequest
		if err := json.Unmarshal([]byte(info.body), &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(payload.AcctIDs) != 1 || payload.AcctIDs[0] != "U1234567" {
			t.Errorf("unexpected acctIds: %v", payload.AcctIDs)
		}
		if len(payload.Conids) != 1 || payload.Conids[0] != 265598 {
			t.Errorf("unexpected conids: %v", payload.Conids)
		}
		if payload.Currency != "USD" {
			t.Errorf("unexpected currency: %q", payload.Currency)
		}
		if payload.Days != 30 {
			t.Errorf("unexpected days: %d", payload.Days)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}
}

func TestListTradesEndpointClearsCookies(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		infoCh <- requestInfo{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			cookie: r.Header.Get("Cookie"),
			body:   string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"execution_id":"e1","symbol":"AAPL","supports_tax_opt":"1","side":"B","order_description":"Bought","trade_time":"20240101-12:00:00","trade_time_r":1700000000,"size":5,"price":"100.00","order_ref":"ref1","submitter":"ib","exchange":"NASDAQ","commission":"1.00","net_amount":-500,"account":"U1","accountCode":"U1","account_allocation_name":"","company_name":"Apple","contract_description_1":"Apple","sec_type":"STK","listing_exchange":"NASDAQ","conid":123,"conidEx":"123","clearing_id":"1","clearing_name":"IB","liquidation_trade":"0","is_event_trading":"0","order_id":42}]`))
	})
	defer server.Close()

	setStaleCookie(t, client, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	query := url.Values{}
	query.Set("days", "2")
	trades, err := client.Orders.ListTrades(ctx, query)
	if err != nil {
		t.Fatalf("list trades: %v", err)
	}
	if len(trades) != 1 || trades[0].ExecutionID != "e1" || trades[0].ContractID != 123 {
		t.Fatalf("unexpected trades response: %#v", trades)
	}

	select {
	case info := <-infoCh:
		if info.method != http.MethodGet {
			t.Errorf("expected GET, got %s", info.method)
		}
		if info.path != "/v1/api/iserver/account/trades" {
			t.Errorf("unexpected path: %s", info.path)
		}
		if info.query != "days=2" {
			t.Errorf("unexpected query: %q", info.query)
		}
		if info.cookie != "" {
			t.Errorf("expected no cookies, got %q", info.cookie)
		}
		if info.body != "" {
			t.Errorf("unexpected body: %q", info.body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for request")
	}
}

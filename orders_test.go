package ibclientportal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPlaceOrdersQuestionThenConfirm(t *testing.T) {
	infoCh := make(chan requestInfo, 2)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		infoCh <- requestInfo{method: r.Method, path: r.URL.Path, body: string(body)}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/api/iserver/account/U1234567/orders":
			_, _ = w.Write([]byte(`[{"id":"c4f9b5a1","message":["You are submitting an order without market data. Are you sure?"],"isSuppressed":false,"messageIds":["o354"]}]`))
		case "/v1/api/iserver/reply/c4f9b5a1":
			_, _ = w.Write([]byte(`[{"order_id":"1234567890","order_status":"PreSubmitted","local_order_id":"returns-1","encrypt_message":"1"}]`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer server.Close()

	ctx := testContext(t)
	placements, err := client.Orders.PlaceOrders(ctx, "U1234567", []OrderRequest{{
		Conid:     265598,
		OrderType: "LMT",
		Side:      "BUY",
		TIF:       "DAY",
		Quantity:  10,
		Price:     123.45,
		COID:      "returns-1",
	}})
	if err != nil {
		t.Fatalf("place orders: %v", err)
	}
	if len(placements) != 1 {
		t.Fatalf("expected 1 placement, got %d: %#v", len(placements), placements)
	}
	q := placements[0]
	if !q.IsQuestion() {
		t.Fatalf("expected a question, got %#v", q)
	}
	if q.IsPlaced() {
		t.Fatalf("a question must not report as placed: %#v", q)
	}
	if q.ReplyID != "c4f9b5a1" {
		t.Errorf("unexpected reply id %q", q.ReplyID)
	}
	if want := "You are submitting an order without market data. Are you sure?"; q.Question() != want {
		t.Errorf("unexpected question %q, want %q", q.Question(), want)
	}

	confirmed, err := client.Orders.ConfirmOrder(ctx, q.ReplyID, true)
	if err != nil {
		t.Fatalf("confirm order: %v", err)
	}
	if len(confirmed) != 1 || !confirmed[0].IsPlaced() {
		t.Fatalf("expected a placed order, got %#v", confirmed)
	}
	if confirmed[0].OrderID != "1234567890" || confirmed[0].OrderStatus != "PreSubmitted" {
		t.Errorf("unexpected placed order: %#v", confirmed[0])
	}
	if confirmed[0].LocalOrderID != "returns-1" {
		t.Errorf("expected the customer order id to be echoed back, got %#v", confirmed[0])
	}

	place := <-infoCh
	if place.method != http.MethodPost {
		t.Errorf("expected POST, got %s", place.method)
	}
	var sent struct {
		Orders []map[string]any `json:"orders"`
	}
	if err := json.Unmarshal([]byte(place.body), &sent); err != nil {
		t.Fatalf("parsing request body %s: %v", place.body, err)
	}
	if len(sent.Orders) != 1 {
		t.Fatalf("expected 1 order in the body, got %s", place.body)
	}
	order := sent.Orders[0]
	if order["orderType"] != "LMT" || order["side"] != "BUY" || order["tif"] != "DAY" {
		t.Errorf("unexpected order body: %s", place.body)
	}
	if order["price"] != 123.45 || order["quantity"] != float64(10) {
		t.Errorf("unexpected price/quantity in body: %s", place.body)
	}
	// Zero-valued optional fields must be omitted, or IB treats an explicit
	// auxPrice/cashQty of 0 as a real instruction.
	for _, key := range []string{"auxPrice", "cashQty", "outsideRTH", "parentId"} {
		if _, ok := order[key]; ok {
			t.Errorf("expected %q to be omitted from the body, got %s", key, place.body)
		}
	}

	reply := <-infoCh
	if reply.method != http.MethodPost {
		t.Errorf("expected POST for the reply, got %s", reply.method)
	}
	if reply.body != `{"confirmed":true}` {
		t.Errorf("unexpected reply body: %s", reply.body)
	}
}

// The gateway reports some rejections as a bare JSON object with HTTP 200
// rather than the documented array. Decoding that into a slice would fail with
// an opaque type error, hiding the message, so the error text must survive.
func TestPlaceOrdersErrorObject(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"Order size exceeds the account's limit"}`))
	})
	defer server.Close()

	placements, err := client.Orders.PlaceOrders(testContext(t), "U1234567", []OrderRequest{{
		Conid: 265598, OrderType: "LMT", Side: "BUY", TIF: "DAY", Quantity: 1e9, Price: 1,
	}})
	if err == nil {
		t.Fatal("expected an error for a rejected order")
	}
	if got, want := err.Error(), "Order size exceeds the account's limit"; !strings.Contains(got, want) {
		t.Errorf("expected error %q to contain %q", got, want)
	}
	if len(placements) != 1 || placements[0].Error == "" {
		t.Errorf("expected the rejection to be returned alongside the error, got %#v", placements)
	}
}

func TestPlaceOrdersValidation(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL.Path)
	})
	defer server.Close()

	ctx := testContext(t)
	if _, err := client.Orders.PlaceOrders(ctx, "", []OrderRequest{{Conid: 1}}); err == nil {
		t.Error("expected an error when the account ID is empty")
	}
	if _, err := client.Orders.PlaceOrders(ctx, "U1234567", nil); err == nil {
		t.Error("expected an error when no orders are given")
	}
	if _, err := client.Orders.ConfirmOrder(ctx, "", true); err == nil {
		t.Error("expected an error when the reply ID is empty")
	}
	if _, err := client.Orders.CancelOrder(ctx, "U1234567", ""); err == nil {
		t.Error("expected an error when the order ID is empty")
	}
	if _, err := client.Orders.WhatIf(ctx, "U1234567", nil); err == nil {
		t.Error("expected an error when no orders are given")
	}
}

func TestCancelOrder(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		infoCh <- requestInfo{method: r.Method, path: r.URL.Path}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"order_id":"1234567890","msg":"Request was submitted","conid":265598,"account":"U1234567"}`))
	})
	defer server.Close()

	resp, err := client.Orders.CancelOrder(testContext(t), "U1234567", "1234567890")
	if err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	if resp.OrderID != "1234567890" || resp.Conid != 265598 {
		t.Errorf("unexpected cancel response: %#v", resp)
	}
	info := <-infoCh
	if info.method != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", info.method)
	}
	if info.path != "/v1/api/iserver/account/U1234567/order/1234567890" {
		t.Errorf("unexpected path: %s", info.path)
	}
}

func TestWhatIf(t *testing.T) {
	infoCh := make(chan requestInfo, 1)
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		infoCh <- requestInfo{method: r.Method, path: r.URL.Path, body: string(body)}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"amount":{"amount":"1,234.50 USD","commission":"1.00 USD","total":"1,235.50 USD"},` +
			`"equity":{"current":"100,000","change":"0","after":"100,000"},` +
			`"initial":{"current":"10,000","change":"617.25","after":"10,617.25"},` +
			`"maintenance":{"current":"8,000","change":"370.35","after":"8,370.35"},` +
			`"position":{"current":"0","change":"10","after":"10"},"warn":"","error":""}`))
	})
	defer server.Close()

	resp, err := client.Orders.WhatIf(testContext(t), "U1234567", []OrderRequest{{
		Conid: 265598, OrderType: "LMT", Side: "BUY", TIF: "DAY", Quantity: 10, Price: 123.45,
	}})
	if err != nil {
		t.Fatalf("whatif: %v", err)
	}
	if resp.Amount.Commission != "1.00 USD" || resp.Amount.Total != "1,235.50 USD" {
		t.Errorf("unexpected amount: %#v", resp.Amount)
	}
	if resp.Initial.After != "10,617.25" || resp.Position.Change != "10" {
		t.Errorf("unexpected margin impact: %#v", resp)
	}
	info := <-infoCh
	if info.path != "/v1/api/iserver/account/U1234567/orders/whatif" {
		t.Errorf("unexpected path: %s", info.path)
	}
}

// IB reports a rejected preview in the body with HTTP 200; that must surface as
// an error rather than an empty preview the caller might trade on.
func TestWhatIfError(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"Order rejected: contract not available for trading"}`))
	})
	defer server.Close()

	_, err := client.Orders.WhatIf(testContext(t), "U1234567", []OrderRequest{{Conid: 1, Quantity: 1}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "contract not available") {
		t.Errorf("unexpected error: %v", err)
	}
}

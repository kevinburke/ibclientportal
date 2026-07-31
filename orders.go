package ibclientportal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// OrderRequest is a single order in a PlaceOrders, ModifyOrder or WhatIf
// request. Only a few fields are required — Conid, OrderType, Side, TIF and
// Quantity, plus Price for a limit order — and the rest are omitted when empty.
//
// https://www.interactivebrokers.com/campus/ibkr-api-page/cpapi-v1/#submit-order
type OrderRequest struct {
	// AcctID is the account to trade in. It may be left empty when the
	// account is already given in the request path.
	AcctID string `json:"acctId,omitempty"`
	// Conid identifies the contract to trade.
	Conid int `json:"conid,omitempty"`
	// ConidEx routes to a specific exchange, in the form "conid@exchange".
	// Set it instead of Conid when the routing matters.
	ConidEx string `json:"conidex,omitempty"`
	// SecType is "conid:secType", e.g. "265598:STK". Optional.
	SecType string `json:"secType,omitempty"`
	// COID is a customer order ID: a caller-chosen identifier, unique for the
	// day, that makes a retried submission idempotent on IB's side.
	COID string `json:"cOID,omitempty"`
	// ParentID ties a child order to a parent order's COID for bracket orders.
	ParentID string `json:"parentId,omitempty"`
	// OrderType is "LMT", "MKT", "STP", "STOP_LIMIT", "MIDPRICE", "TRAIL" or
	// "TRAILLMT".
	OrderType string `json:"orderType,omitempty"`
	// ListingExchange is the routing destination, e.g. "SMART" (the default).
	ListingExchange string `json:"listingExchange,omitempty"`
	// OutsideRTH allows the order to trade outside regular trading hours.
	OutsideRTH bool `json:"outsideRTH,omitempty"`
	// Price is the limit price for LMT and STOP_LIMIT orders.
	Price float64 `json:"price,omitempty"`
	// AuxPrice is the stop price for STP and STOP_LIMIT orders.
	AuxPrice float64 `json:"auxPrice,omitempty"`
	// Side is "BUY" or "SELL".
	Side string `json:"side,omitempty"`
	// Ticker is the contract's symbol. Optional; the conid is authoritative.
	Ticker string `json:"ticker,omitempty"`
	// TIF is the time in force: "DAY", "GTC", "OPG" or "IOC".
	TIF string `json:"tif,omitempty"`
	// Quantity is the number of shares or contracts.
	Quantity float64 `json:"quantity,omitempty"`
	// CashQty places the order by cash value instead of quantity.
	CashQty float64 `json:"cashQty,omitempty"`
	// UseAdaptive applies IB's Price Management Algo to the order.
	UseAdaptive bool `json:"useAdaptive,omitempty"`
	// Referrer is a free-form tag recorded with the order.
	Referrer string `json:"referrer,omitempty"`
}

// OrderPlacement is one element of the response to placing, modifying or
// confirming an order. The endpoint returns a union of three shapes and which
// one arrived is not signalled by any type field, so check with IsQuestion and
// IsPlaced rather than reading fields blind:
//
//   - a question the caller must answer before the order is transmitted
//     (ReplyID and Messages set) — answer it with ConfirmOrder;
//   - a placed order (OrderID and OrderStatus set);
//   - an error (Error set), which the gateway sometimes returns with HTTP 200.
type OrderPlacement struct {
	// ReplyID identifies a question to answer via ConfirmOrder.
	ReplyID string `json:"id,omitempty"`
	// Messages holds the text of a question, one entry per paragraph.
	Messages []string `json:"message,omitempty"`
	// MessageIDs holds IB's identifiers for the question, e.g. "o163". They
	// are stable across orders and can be used to recognise a known question.
	MessageIDs []string `json:"messageIds,omitempty"`
	// IsSuppressed reports whether this message class was suppressed.
	IsSuppressed bool `json:"isSuppressed,omitempty"`

	// OrderID is IB's identifier for a successfully placed order.
	OrderID string `json:"order_id,omitempty"`
	// OrderStatus is the order's state, e.g. "PreSubmitted" or "Submitted".
	OrderStatus string `json:"order_status,omitempty"`
	// LocalOrderID echoes the COID from the request, when one was set.
	LocalOrderID string `json:"local_order_id,omitempty"`
	// EncryptMessage is an IB bookkeeping field.
	EncryptMessage string `json:"encrypt_message,omitempty"`
	// Warning carries a non-fatal message about an accepted order.
	Warning string `json:"warning_message,omitempty"`
	// Text accompanies some error responses.
	Text string `json:"text,omitempty"`
	// Error is set when the gateway rejected the request.
	Error string `json:"error,omitempty"`
}

// IsQuestion reports whether this element is a question that must be answered
// with ConfirmOrder before the order is transmitted.
func (p OrderPlacement) IsQuestion() bool {
	return p.ReplyID != "" && p.OrderID == ""
}

// IsPlaced reports whether this element describes an order IB accepted.
func (p OrderPlacement) IsPlaced() bool {
	return p.OrderID != ""
}

// Question returns the question text as a single string, with one line per
// paragraph. It returns the empty string if this element is not a question.
func (p OrderPlacement) Question() string {
	if !p.IsQuestion() {
		return ""
	}
	return strings.Join(p.Messages, "\n")
}

// PlaceOrders submits one or more orders to the given account.
//
// ListTradableAccounts must have been called at least once in this session
// first; IB rejects order submission otherwise.
//
// The response is a union — see OrderPlacement. In particular, a successful
// call does NOT mean the order was transmitted: IB commonly answers with a
// question ("this order will be placed outside regular trading hours", a size
// or price cap warning, and so on) which must be confirmed with ConfirmOrder
// before anything reaches the market. Confirming a question can produce another
// question, so loop until every element reports IsPlaced.
func (o *OrdersService) PlaceOrders(ctx context.Context, accountID string, orders []OrderRequest) ([]OrderPlacement, error) {
	if accountID == "" {
		return nil, fmt.Errorf("ibclientportal: PlaceOrders: no account ID given")
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("ibclientportal: PlaceOrders: no orders given")
	}
	path := "/iserver/account/" + url.PathEscape(accountID) + "/orders"
	body := struct {
		Orders []OrderRequest `json:"orders"`
	}{Orders: orders}
	return o.placementRequest(ctx, path, body)
}

// ModifyOrder replaces a live order's terms (price, quantity, time in force).
// The response is the same union as PlaceOrders, questions included.
func (o *OrdersService) ModifyOrder(ctx context.Context, accountID, orderID string, order OrderRequest) ([]OrderPlacement, error) {
	if accountID == "" {
		return nil, fmt.Errorf("ibclientportal: ModifyOrder: no account ID given")
	}
	if orderID == "" {
		return nil, fmt.Errorf("ibclientportal: ModifyOrder: no order ID given")
	}
	path := "/iserver/account/" + url.PathEscape(accountID) + "/order/" + url.PathEscape(orderID)
	return o.placementRequest(ctx, path, order)
}

// ConfirmOrder answers a question raised by PlaceOrders or ModifyOrder.
// Passing confirmed=false abandons the order.
//
// The reply may itself be another question, so callers should keep answering
// until the response reports a placed order (or an error).
func (o *OrdersService) ConfirmOrder(ctx context.Context, replyID string, confirmed bool) ([]OrderPlacement, error) {
	if replyID == "" {
		return nil, fmt.Errorf("ibclientportal: ConfirmOrder: no reply ID given")
	}
	path := "/iserver/reply/" + url.PathEscape(replyID)
	body := struct {
		Confirmed bool `json:"confirmed"`
	}{Confirmed: confirmed}
	return o.placementRequest(ctx, path, body)
}

// placementRequest POSTs an order-shaped request and decodes the union
// response. The gateway answers with a JSON array in the normal case but with
// a bare object for some errors, so decode into a RawMessage first: unmarshaling
// an object straight into a slice would fail with an opaque type error and hide
// the message IB is trying to report.
func (o *OrdersService) placementRequest(ctx context.Context, path string, body any) ([]OrderPlacement, error) {
	var raw json.RawMessage
	if err := o.client.UpdateResource(ctx, path, body, &raw); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, fmt.Errorf("ibclientportal: %s: empty response from the gateway", path)
	}
	if trimmed[0] == '[' {
		var placements []OrderPlacement
		if err := json.Unmarshal(raw, &placements); err != nil {
			return nil, fmt.Errorf("ibclientportal: %s: parsing response %s: %w", path, raw, err)
		}
		for _, p := range placements {
			if p.Error != "" {
				return placements, fmt.Errorf("ibclientportal: %s: %s", path, p.Error)
			}
		}
		return placements, nil
	}
	var single OrderPlacement
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("ibclientportal: %s: parsing response %s: %w", path, raw, err)
	}
	if single.Error != "" {
		return []OrderPlacement{single}, fmt.Errorf("ibclientportal: %s: %s", path, single.Error)
	}
	return []OrderPlacement{single}, nil
}

// CancelOrderResponse is the response from cancelling a live order.
type CancelOrderResponse struct {
	OrderID string `json:"order_id"`
	Message string `json:"msg"`
	Conid   int64  `json:"conid"`
	Account string `json:"account"`
	Error   string `json:"error"`
}

// CancelOrder cancels a live order. IB acknowledges the cancel request
// synchronously; the order reaches a cancelled state asynchronously, so poll
// ListOrders to confirm it actually went away.
func (o *OrdersService) CancelOrder(ctx context.Context, accountID, orderID string) (CancelOrderResponse, error) {
	var val CancelOrderResponse
	if accountID == "" {
		return val, fmt.Errorf("ibclientportal: CancelOrder: no account ID given")
	}
	if orderID == "" {
		return val, fmt.Errorf("ibclientportal: CancelOrder: no order ID given")
	}
	path := "/iserver/account/" + url.PathEscape(accountID) + "/order/" + url.PathEscape(orderID)
	err := o.client.MakeRequest(ctx, "DELETE", path, nil, nil, &val)
	if err == nil && val.Error != "" {
		return val, fmt.Errorf("ibclientportal: CancelOrder: %s", val.Error)
	}
	return val, err
}

// WhatIfAmount is a monetary breakdown in a WhatIfResponse. IB returns these
// as preformatted strings, currency symbols and all, rather than numbers.
type WhatIfAmount struct {
	Amount     string `json:"amount"`
	Commission string `json:"commission"`
	Total      string `json:"total"`
}

// WhatIfChange is a before/after/delta triple in a WhatIfResponse.
type WhatIfChange struct {
	Current string `json:"current"`
	Change  string `json:"change"`
	After   string `json:"after"`
}

// WhatIfResponse previews the effect of an order: its cost including estimated
// commission, and what it does to the account's equity and margin.
type WhatIfResponse struct {
	Amount      WhatIfAmount `json:"amount"`
	Equity      WhatIfChange `json:"equity"`
	Initial     WhatIfChange `json:"initial"`
	Maintenance WhatIfChange `json:"maintenance"`
	Position    WhatIfChange `json:"position"`
	Warn        string       `json:"warn"`
	Error       string       `json:"error"`
}

// WhatIf previews an order without placing it, returning its estimated cost and
// its effect on the account's margin. Use it as a pre-trade check: it is the
// only way to see IB's own commission estimate and margin impact before
// committing.
//
// It returns an error if IB reports one in the response body, which it does
// with HTTP 200.
func (o *OrdersService) WhatIf(ctx context.Context, accountID string, orders []OrderRequest) (WhatIfResponse, error) {
	var val WhatIfResponse
	if accountID == "" {
		return val, fmt.Errorf("ibclientportal: WhatIf: no account ID given")
	}
	if len(orders) == 0 {
		return val, fmt.Errorf("ibclientportal: WhatIf: no orders given")
	}
	path := "/iserver/account/" + url.PathEscape(accountID) + "/orders/whatif"
	body := struct {
		Orders []OrderRequest `json:"orders"`
	}{Orders: orders}
	if err := o.client.UpdateResource(ctx, path, body, &val); err != nil {
		return val, err
	}
	if val.Error != "" {
		return val, fmt.Errorf("ibclientportal: WhatIf: %s", val.Error)
	}
	return val, nil
}

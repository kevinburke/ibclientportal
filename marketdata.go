package ibclientportal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Snapshot is a single contract's market data as returned by
// /iserver/marketdata/snapshot. Like a streaming MarketDataUpdate it carries
// raw field values keyed by IB's numeric field codes (see the Field*
// constants); read them with String or Float.
type Snapshot struct {
	// Conid is the contract identifier this row is for.
	Conid int
	// Fields maps IB numeric field codes to their raw JSON values. A field the
	// gateway has not resolved yet is absent rather than zero.
	Fields map[string]json.RawMessage
}

// String returns the value of the given field code as a string, stripping
// surrounding quotes if the underlying JSON value was a string. The second
// return value reports whether the field was present.
func (s Snapshot) String(field string) (string, bool) {
	return fieldString(s.Fields, field)
}

// Float returns the value of the given field code as a float64, handling both
// JSON-number and JSON-string encodings. The second return value reports
// whether the field was present and parseable as a number.
func (s Snapshot) Float(field string) (float64, bool) {
	return fieldFloat(s.Fields, field)
}

// fieldString reads a raw field value as a string. Shared by Snapshot and
// MarketDataUpdate so the REST and streaming representations parse identically.
func fieldString(fields map[string]json.RawMessage, field string) (string, bool) {
	raw, ok := fields[field]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	return string(raw), true
}

// fieldFloat reads a raw field value as a float64.
func fieldFloat(fields map[string]json.RawMessage, field string) (float64, bool) {
	s, ok := fieldString(fields, field)
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

// Snapshot returns a live market-data snapshot for the given contracts. If
// fields is empty a default set (last, bid, ask, and their sizes) is requested.
//
// Two caveats from IBKR, both of which the caller must handle:
//
//   - /iserver/accounts must have been called at least once in this session, and
//     for derivative contracts /iserver/secdef/search as well, or the gateway
//     returns nothing.
//   - The first call for a contract typically returns little more than the
//     conid while the backend subscribes to the feed. Poll until the field you
//     want appears rather than treating one empty response as "no data".
//
// A snapshot consumes one of the account's concurrent market-data lines, from
// the same pool as streaming subscriptions, until it is released with
// Unsubscribe or UnsubscribeAll.
func (m *MarketDataService) Snapshot(ctx context.Context, conids []int, fields []string) ([]Snapshot, error) {
	if len(conids) == 0 {
		return nil, fmt.Errorf("ibclientportal: Snapshot: no conids given")
	}
	if len(fields) == 0 {
		fields = []string{
			FieldLastPrice, FieldBidPrice, FieldAskPrice,
			FieldBidSize, FieldAskSize, FieldVolume,
		}
	}
	ids := make([]string, len(conids))
	for i, conid := range conids {
		ids[i] = strconv.Itoa(conid)
	}
	query := url.Values{
		"conids": []string{strings.Join(ids, ",")},
		"fields": []string{strings.Join(fields, ",")},
	}
	var rows []map[string]json.RawMessage
	if err := m.client.ListResource(ctx, "/iserver/marketdata/snapshot", query, &rows); err != nil {
		return nil, err
	}
	snapshots := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		conid := 0
		if raw, ok := row["conid"]; ok {
			// The conid is a JSON number here, but tolerate a string.
			if err := json.Unmarshal(raw, &conid); err != nil {
				var s string
				if err := json.Unmarshal(raw, &s); err == nil {
					conid, _ = strconv.Atoi(s)
				}
			}
		}
		fieldsCopy := make(map[string]json.RawMessage, len(row))
		for k, v := range row {
			switch k {
			case "conid", "conidEx", "server_id", "_updated":
				continue
			}
			fieldsCopy[k] = v
		}
		snapshots = append(snapshots, Snapshot{Conid: conid, Fields: fieldsCopy})
	}
	return snapshots, nil
}

// Unsubscribe releases the market-data line held for one contract, whether it
// was taken by a REST snapshot or a streaming subscription.
func (m *MarketDataService) Unsubscribe(ctx context.Context, conid int) error {
	var resp json.RawMessage
	return m.client.UpdateResource(ctx, "/iserver/marketdata/unsubscribe",
		map[string]int{"conid": conid}, &resp)
}

// UnsubscribeAll releases every market-data line held by this session. It is
// the belt-and-braces cleanup for a client that has been subscribing and
// unsubscribing over time: an unsubscribe lost with a dropped websocket leaves
// the line held on the gateway side until the session ends.
func (m *MarketDataService) UnsubscribeAll(ctx context.Context) error {
	var resp json.RawMessage
	return m.client.ListResource(ctx, "/iserver/marketdata/unsubscribeall", nil, &resp)
}

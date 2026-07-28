package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPickUSContract(t *testing.T) {
	var stocks []stock
	const body = `[{
		"name": "VANGUARD S&P 500 ETF",
		"assetClass": "STK",
		"contracts": [
			{"conid": 136155092, "exchange": "MEXI", "isUS": false},
			{"conid": 136155102, "exchange": "ARCA", "isUS": true}
		]
	}]`
	if err := json.Unmarshal([]byte(body), &stocks); err != nil {
		t.Fatal(err)
	}
	got, ok := pickUSContract(stocks)
	if !ok {
		t.Fatal("pickUSContract: no US contract found")
	}
	if got.Conid != 136155102 {
		t.Errorf("pickUSContract conid = %d, want the US listing 136155102", got.Conid)
	}
	if got.Exchange != "ARCA" {
		t.Errorf("pickUSContract exchange = %q, want ARCA", got.Exchange)
	}

	if _, ok := pickUSContract(nil); ok {
		t.Error("pickUSContract(nil) = ok, want not ok")
	}
	// A symbol that only lists abroad must not be silently used.
	foreign := []stock{{AssetClass: "STK", Contracts: []struct {
		Conid    int    `json:"conid"`
		Exchange string `json:"exchange"`
		IsUS     bool   `json:"isUS"`
	}{{Conid: 1, Exchange: "MEXI"}}}}
	if _, ok := pickUSContract(foreign); ok {
		t.Error("pickUSContract(foreign only) = ok, want not ok")
	}
}

func TestRowForConid(t *testing.T) {
	var rows []map[string]json.RawMessage
	const body = `[{"conid": 111, "31": "1.00"}, {"conid": 222, "31": "2.00"}]`
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatal(err)
	}
	row := rowForConid(rows, 222)
	if got := jsonString(row[fieldLastPrice]); got != "2.00" {
		t.Errorf("rowForConid(222) last price = %q, want 2.00", got)
	}
	if row := rowForConid(rows, 333); row != nil {
		t.Errorf("rowForConid(333) = %v, want nil", row)
	}
	// The gateway sometimes omits the conid; a single row is unambiguous.
	single := []map[string]json.RawMessage{{"31": json.RawMessage(`"9.00"`)}}
	if got := jsonString(rowForConid(single, 444)[fieldLastPrice]); got != "9.00" {
		t.Errorf("rowForConid(single row) last price = %q, want 9.00", got)
	}
}

func TestJSONString(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`"C123.45"`, "C123.45"},
		{`123.45`, "123.45"},
		{`"R"`, "R"},
		{``, ""},
	}
	for _, tt := range tests {
		if got := jsonString(json.RawMessage(tt.raw)); got != tt.want {
			t.Errorf("jsonString(%s) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestFirstPrice(t *testing.T) {
	// Only a bid is present: still a usable answer to "did the snapshot work".
	row := map[string]json.RawMessage{fieldBidPrice: json.RawMessage(`"10.5"`)}
	if got := firstPrice(row); got != "10.5" {
		t.Errorf("firstPrice = %q, want 10.5", got)
	}
	// Availability alone is not a price: the snapshot did not deliver a quote.
	row = map[string]json.RawMessage{fieldAvailability: json.RawMessage(`"N"`)}
	if got := firstPrice(row); got != "" {
		t.Errorf("firstPrice(no price fields) = %q, want empty", got)
	}
}

func TestVerdict(t *testing.T) {
	ok := &SnapshotRun{Succeeded: true}
	silent := &SnapshotRun{}
	failed := &SnapshotRun{Attempts: []SnapshotAttempt{{Error: "429 too many requests"}}}

	tests := []struct {
		name string
		in   *Result
		want string
	}{
		{"limit not reached", &Result{Baseline: ok, Saturated: ok, MarketMoving: true}, "limit was not reached"},
		{"displacement", &Result{Baseline: ok, Saturated: ok, MarketMoving: true, WentSilent: []int{1, 2}}, "displace"},
		{"market closed", &Result{Baseline: ok, Saturated: ok, WentSilent: []int{1, 2}}, "indeterminate"},
		{"explicit error", &Result{Baseline: ok, Saturated: failed}, "rejects requests"},
		{"silent failure", &Result{Baseline: ok, Saturated: silent}, "fail silently"},
		{"bad baseline", &Result{Baseline: silent, Saturated: silent}, "inconclusive"},
		{"no run", &Result{}, "inconclusive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := verdict(tt.in); !strings.Contains(got, tt.want) {
				t.Errorf("verdict = %q, want it to mention %q", got, tt.want)
			}
		})
	}
}

func TestSplitSymbols(t *testing.T) {
	got := splitSymbols(" aapl, msft ,, nvda ")
	want := []string{"AAPL", "MSFT", "NVDA"}
	if len(got) != len(want) {
		t.Fatalf("splitSymbols = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitSymbols = %v, want %v", got, want)
		}
	}
	if got := splitSymbols("  "); got != nil {
		t.Errorf("splitSymbols(blank) = %v, want nil", got)
	}
}

// The default symbol pool must be able to fill the default --count plus the
// unsubscribed probe contract, with slack for symbols that fail to resolve.
func TestDefaultSymbolsCoverDefaultCount(t *testing.T) {
	syms := strings.Fields(defaultSymbols)
	if len(syms) < 101 {
		t.Fatalf("defaultSymbols has %d symbols, need at least 101", len(syms))
	}
	seen := make(map[string]bool, len(syms))
	for _, s := range syms {
		if seen[s] {
			t.Errorf("defaultSymbols repeats %s", s)
		}
		seen[s] = true
	}
}

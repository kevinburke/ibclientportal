package ibclientportal

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"testing"
	"time"
)

func testHost() string {
	if host := os.Getenv("IBCLIENTPORTAL_TEST_HOST"); host != "" {
		return host
	}
	return ""
}

func TestOrdersParsing(t *testing.T) {
	var resp OrdersResponse
	if err := json.Unmarshal(ordersResponse, &resp); err != nil {
		t.Fatalf("failed to parse orders response: %v", err)
	}
	if !resp.Snapshot {
		t.Errorf("expected Snapshot true, got false")
	}
	if len(resp.Orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(resp.Orders))
	}
	order := resp.Orders[0]
	if order.Account != "U1234567" {
		t.Errorf("expected Account U1234567, got %s", order.Account)
	}
	if order.ContractID != 265598 {
		t.Errorf("expected ContractID 265598, got %d", order.ContractID)
	}
	if order.OrderID != 1234568790 {
		t.Errorf("expected OrderID 1234568790, got %d", order.OrderID)
	}
	if order.Ticker != "AAPL" {
		t.Errorf("expected Ticker AAPL, got %s", order.Ticker)
	}
	if order.Status != "Filled" {
		t.Errorf("expected Status Filled, got %s", order.Status)
	}
	if order.FilledQuantity != 5.0 {
		t.Errorf("expected FilledQuantity 5.0, got %f", order.FilledQuantity)
	}
	if order.Side != "SELL" {
		t.Errorf("expected Side SELL, got %s", order.Side)
	}
	if order.AvgPrice != "192.26" {
		t.Errorf("expected AvgPrice 192.26, got %s", order.AvgPrice)
	}
	if order.TimeInForce != "GTC" {
		t.Errorf("expected TimeInForce GTC, got %s", order.TimeInForce)
	}
	if order.LastExecutionTimeR != 1702317649000 {
		t.Errorf("expected LastExecutionTime_r 1702317649000, got %d", order.LastExecutionTimeR)
	}
	if order.OrderRef != "Order123" {
		t.Errorf("expected OrderRef Order123, got %s", order.OrderRef)
	}
}

func TestPositionsParsing(t *testing.T) {
	var positions []Position
	if err := json.Unmarshal(positionsResponse, &positions); err != nil {
		t.Fatalf("failed to parse positions response: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	pos := positions[0]
	if pos.Position != 12.0 {
		t.Errorf("expected Position 12.0, got %f", pos.Position)
	}
	if pos.ContractID != 9408 {
		t.Errorf("expected ContractID 9408, got %d", pos.ContractID)
	}
	if pos.Currency != "USD" {
		t.Errorf("expected Currency USD, got %s", pos.Currency)
	}
	if pos.Description != "MCD" {
		t.Errorf("expected Description MCD, got %s", pos.Description)
	}
	if pos.SecType != "STK" {
		t.Errorf("expected SecType STK, got %s", pos.SecType)
	}
	if pos.Timestamp != 1717444668 {
		t.Errorf("expected Timestamp 1717444668, got %d", pos.Timestamp)
	}
	if pos.Sector != "Consumer, Cyclical" {
		t.Errorf("expected Sector 'Consumer, Cyclical', got %s", pos.Sector)
	}
	if pos.Group != "Retail" {
		t.Errorf("expected Group Retail, got %s", pos.Group)
	}
	if pos.IsLastToLiq != false {
		t.Errorf("expected IsLastToLiq false, got %v", pos.IsLastToLiq)
	}
}

func TestAccountsParsing(t *testing.T) {
	var accounts []Account
	if err := json.Unmarshal(accountsResponse, &accounts); err != nil {
		t.Fatalf("failed to parse accounts response: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	acct := accounts[0]
	if acct.ID != "U1234567" {
		t.Errorf("expected ID U1234567, got %s", acct.ID)
	}
	if acct.AccountID != "U1234567" {
		t.Errorf("expected AccountID U1234567, got %s", acct.AccountID)
	}
	if acct.Currency != "USD" {
		t.Errorf("expected Currency USD, got %s", acct.Currency)
	}
	if acct.Type != "DEMO" {
		t.Errorf("expected Type DEMO, got %s", acct.Type)
	}
	if acct.ClearingStatus != "O" {
		t.Errorf("expected ClearingStatus O, got %s", acct.ClearingStatus)
	}
	if acct.AccountStatus != 1644814800000 {
		t.Errorf("expected AccountStatus 1644814800000, got %d", acct.AccountStatus)
	}
	if acct.TrackVirtualFXPortfolio != true {
		t.Errorf("expected TrackVirtualFXPortfolio true, got %v", acct.TrackVirtualFXPortfolio)
	}
	if acct.BrokerageAccess != true {
		t.Errorf("expected BrokerageAccess true, got %v", acct.BrokerageAccess)
	}
	if acct.AccountAlias != nil {
		t.Errorf("expected AccountAlias nil, got %v", acct.AccountAlias)
	}
	if acct.Parent.IsMultiplex != false {
		t.Errorf("expected Parent.IsMultiplex false, got %v", acct.Parent.IsMultiplex)
	}
}

func TestStocks(t *testing.T) {
	c := New(testHost())
	c.SetInsecureSkipVerify()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query := url.Values{}
	query.Set("symbols", "VOO,VT")
	stocks, err := c.Contracts.Stocks(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	voo, ok := stocks["VOO"]
	if !ok {
		t.Fatalf("unexpected response: %#v", stocks)
	}
	if len(voo) != 1 {
		t.Errorf("expected one results, got %d", len(voo))
	}
	if len(voo[0].Contracts) != 2 {
		t.Errorf("expected two constracts, got %d", len(voo[0].Contracts))
	}
	count := 0
	for _, contract := range voo[0].Contracts {
		if contract.IsUS {
			count++
		}
		if contract.ContractID <= 0 {
			t.Errorf("invalid contract id: %#v", contract)
		}
		if contract.Exchange == "" {
			t.Errorf("invalid exchange: %#v", contract)
		}
	}
	if count != 1 {
		t.Errorf("incorrect number of in-US results: %#v", voo[0].Contracts)
	}
}

func TestMarketData(t *testing.T) {
	c := New(testHost())
	c.SetInsecureSkipVerify()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	query := url.Values{}
	query.Set("conid", "136155102")
	query.Set("period", "10d")
	hist, err := c.MarketData.History(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if hist == nil {
		t.Fatal("expected market data response, got nil")
	}
	if hist.Symbol == "" {
		t.Fatalf("expected symbol in response: %#v", hist)
	}
	if len(hist.Data) == 0 {
		t.Fatalf("expected market data points: %#v", hist)
	}
	if hist.Data[0].Time.IsZero() {
		t.Fatalf("expected non-zero data time: %#v", hist.Data[0])
	}
}

func TestSearch(t *testing.T) {
	c := New(testHost())
	c.SetInsecureSkipVerify()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hist, err := c.SecurityDefinitions.Search(ctx, SecurityDefinitionSearchParameters{
		Symbol: "VMNVX",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 {
		t.Fatalf("expected search results, got %#v", hist)
	}
	if hist[0].ContractID == 0 {
		t.Fatalf("expected contract id in search result: %#v", hist[0])
	}
}

func TestTickle(t *testing.T) {
	c := New(testHost())
	c.SetInsecureSkipVerify()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.Tickle(ctx, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Session == "" {
		t.Fatalf("expected session in response: %#v", resp)
	}
	if resp.SSOExpires <= 0 {
		t.Fatalf("expected sso expires in response: %#v", resp)
	}
}

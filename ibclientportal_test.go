package ibclientportal

import (
	"context"
	"net/url"
	"testing"
	"time"
)

func TestStocks(t *testing.T) {
	c := New("")
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

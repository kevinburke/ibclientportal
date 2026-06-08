package flex

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kevinburke/ibclientportal"
)

// A trimmed Activity Flex Query report containing a Cash Transactions section
// with a deposit, a withdrawal and a fee. Account number and amounts are
// fictional.
var cashTransactionsXML = []byte(`<FlexQueryResponse queryName="Cash" type="AF">
<FlexStatements count="1">
<FlexStatement accountId="U1234567" fromDate="20240101" toDate="20240131" period="LastMonth" whenGenerated="20240201;120000">
<CashTransactions>
<CashTransaction accountId="U1234567" currency="USD" assetCategory="" amount="5000" dateTime="20240105;202000" settleDate="20240105" description="CASH RECEIPTS / ELECTRONIC FUND TRANSFERS" type="Deposits/Withdrawals" transactionID="111" reportDate="20240105" fxRateToBase="1" levelOfDetail="DETAIL" />
<CashTransaction accountId="U1234567" currency="USD" assetCategory="" amount="-1200.50" dateTime="20240120;202000" settleDate="20240120" description="DISBURSEMENT INITIATED BY ACH" type="Deposits/Withdrawals" transactionID="222" reportDate="20240120" fxRateToBase="1" levelOfDetail="DETAIL" />
<CashTransaction accountId="U1234567" currency="USD" assetCategory="" amount="-0.35" dateTime="20240131;202000" settleDate="20240131" description="SNAPSHOT MARKET DATA FEE" type="Other Fees" transactionID="333" reportDate="20240131" fxRateToBase="1" levelOfDetail="DETAIL" />
</CashTransactions>
</FlexStatement>
</FlexStatements>
</FlexQueryResponse>`)

func TestCashTransactionsParsing(t *testing.T) {
	var resp Report
	if err := xml.Unmarshal(cashTransactionsXML, &resp); err != nil {
		t.Fatalf("failed to parse flex response: %v", err)
	}
	if resp.QueryName != "Cash" {
		t.Errorf("expected QueryName Cash, got %s", resp.QueryName)
	}
	if len(resp.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(resp.Statements))
	}
	stmt := resp.Statements[0]
	if stmt.AccountID != "U1234567" {
		t.Errorf("expected AccountID U1234567, got %s", stmt.AccountID)
	}
	if stmt.FromDate != "20240101" || stmt.ToDate != "20240131" {
		t.Errorf("unexpected date range %s..%s", stmt.FromDate, stmt.ToDate)
	}

	txns := resp.CashTransactions()
	if len(txns) != 3 {
		t.Fatalf("expected 3 cash transactions, got %d", len(txns))
	}

	deposit := txns[0]
	if deposit.Type != CashActionDepositsWithdrawals {
		t.Errorf("expected Type %q, got %q", CashActionDepositsWithdrawals, deposit.Type)
	}
	if deposit.Amount != 5000 {
		t.Errorf("expected Amount 5000, got %f", deposit.Amount)
	}
	if deposit.Currency != "USD" {
		t.Errorf("expected Currency USD, got %s", deposit.Currency)
	}
	if deposit.Description != "CASH RECEIPTS / ELECTRONIC FUND TRANSFERS" {
		t.Errorf("expected deposit description, got %s", deposit.Description)
	}
	if deposit.TransactionID != "111" {
		t.Errorf("expected TransactionID 111, got %s", deposit.TransactionID)
	}

	withdrawal := txns[1]
	if withdrawal.Amount != -1200.50 {
		t.Errorf("expected Amount -1200.50, got %f", withdrawal.Amount)
	}

	fee := txns[2]
	if fee.Type != CashActionOtherFees {
		t.Errorf("expected Type %q, got %q", CashActionOtherFees, fee.Type)
	}
	if fee.Amount != -0.35 {
		t.Errorf("expected Amount -0.35, got %f", fee.Amount)
	}
}

// TestDownload exercises the full two-step download flow, including a transient
// "statement generation in progress" response that Download must retry past.
func TestDownload(t *testing.T) {
	const token = "tok-abc"
	const queryID = "998877"
	const referenceCode = "1234567890"

	var getStatementCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("v") != apiVersion {
			t.Errorf("expected version %s, got %s", apiVersion, q.Get("v"))
		}
		if q.Get("t") != token {
			t.Errorf("expected token %s, got %s", token, q.Get("t"))
		}
		if ua := r.Header.Get("User-Agent"); ua != ibclientportal.UserAgent {
			t.Errorf("expected User-Agent %s, got %s", ibclientportal.UserAgent, ua)
		}
		w.Header().Set("Content-Type", "text/xml")
		switch r.URL.Path {
		case "/SendRequest":
			if q.Get("q") != queryID {
				t.Errorf("SendRequest: expected query %s, got %s", queryID, q.Get("q"))
			}
			w.Write([]byte(`<FlexStatementResponse timestamp="01 February, 2024 12:00 PM EST"><Status>Success</Status><ReferenceCode>` + referenceCode + `</ReferenceCode><Url>REPLACED</Url></FlexStatementResponse>`))
		case "/GetStatement":
			if q.Get("q") != referenceCode {
				t.Errorf("GetStatement: expected reference code %s, got %s", referenceCode, q.Get("q"))
			}
			getStatementCalls++
			if getStatementCalls == 1 {
				// Report still generating: server should be retried.
				w.Write([]byte(`<FlexStatementResponse timestamp="01 February, 2024 12:00 PM EST"><Status>Warn</Status><ErrorCode>1019</ErrorCode><ErrorMessage>Statement generation in progress. Please try again shortly.</ErrorMessage></FlexStatementResponse>`))
				return
			}
			w.Write(cashTransactionsXML)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(token)
	c.requestURL = srv.URL + "/SendRequest"
	c.statementURL = srv.URL + "/GetStatement"
	// Shrink the retry delay so the test does not sleep for the real 5 seconds.
	c.retryDelay = time.Millisecond
	resp, err := c.Download(context.Background(), queryID)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if getStatementCalls != 2 {
		t.Errorf("expected 2 GetStatement calls (one retry), got %d", getStatementCalls)
	}
	txns := resp.CashTransactions()
	if len(txns) != 3 {
		t.Fatalf("expected 3 cash transactions, got %d", len(txns))
	}
}

func TestSendRequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<FlexStatementResponse timestamp="01 February, 2024 12:00 PM EST"><Status>Fail</Status><ErrorCode>1015</ErrorCode><ErrorMessage>Token is invalid.</ErrorMessage></FlexStatementResponse>`))
	}))
	defer srv.Close()

	c := NewClient("bad-token")
	c.requestURL = srv.URL
	_, err := c.SendRequest(context.Background(), "123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var ferr *Error
	if !errors.As(err, &ferr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if ferr.Code != "1015" {
		t.Errorf("expected code 1015, got %s", ferr.Code)
	}
	if ferr.Retryable() {
		t.Errorf("invalid-token error should not be retryable")
	}
}

func TestBadResponseSurfacesBody(t *testing.T) {
	const htmlBody = "<html><body>Access denied</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(htmlBody))
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.statementURL = srv.URL
	_, err := c.GetStatement(context.Background(), "ref")
	if err == nil {
		t.Fatal("expected error for non-Flex response, got nil")
	}
	var bad *BadResponseError
	if !errors.As(err, &bad) {
		t.Fatalf("expected *BadResponseError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "Access denied") {
		t.Errorf("expected error to surface the response body, got %q", err.Error())
	}
}

func TestRetryDelay(t *testing.T) {
	cases := []struct {
		code      string
		retryable bool
	}{
		{"1001", true},
		{"1003", false},
		{"1004", true},
		{"1005", true},
		{"1006", true},
		{"1007", true},
		{"1008", true},
		{"1009", true},
		{"1018", true},
		{"1019", true},
		{"1021", true},
		{"1015", false},
		{"1012", false},
	}
	for _, tc := range cases {
		e := &Error{Code: tc.code}
		if got := e.Retryable(); got != tc.retryable {
			t.Errorf("code %s: Retryable() = %v, want %v", tc.code, got, tc.retryable)
		}
	}
}

func TestGetWiringQueryParams(t *testing.T) {
	// Confirm the version/token/query parameters are encoded exactly once and
	// existing URL query parameters are preserved.
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Write([]byte(`<FlexStatementResponse timestamp="01 February, 2024 12:00 PM EST"><Status>Success</Status><ReferenceCode>1</ReferenceCode><Url>x</Url></FlexStatementResponse>`))
	}))
	defer srv.Close()

	c := NewClient("tok")
	c.requestURL = srv.URL + "/SendRequest?extra=1"
	if _, err := c.SendRequest(context.Background(), "Q1"); err != nil {
		t.Fatalf("SendRequest: %v", err)
	}
	if gotQuery.Get("extra") != "1" {
		t.Errorf("expected existing query param extra=1 to be preserved")
	}
	if gotQuery.Get("v") != "3" || gotQuery.Get("t") != "tok" || gotQuery.Get("q") != "Q1" {
		t.Errorf("unexpected flex params: %v", gotQuery)
	}
}

// fullSectionsXML exercises the structural edge cases of the generated model:
// the shared Trade shape across levelOfDetail rows, an empty numeric attribute
// (strike) decoding to 0 via Float, the self-nesting OptionEAE section, the
// FxPositions/FxLots slice, and RawElement capture of an otherwise-empty
// section. All values are fictional; security names are reused.
var fullSectionsXML = []byte(`<FlexQueryResponse queryName="full" type="AF">
<FlexStatements count="1">
<FlexStatement accountId="U1" fromDate="20260101" toDate="20260131">
<Trades>
<Trade symbol="AAPL" conid="265598" assetCategory="STK" quantity="10" tradePrice="150.5" proceeds="-1505" ibCommission="-1" strike="" buySell="BUY" levelOfDetail="EXECUTION" />
<Lot symbol="AAPL" quantity="10" tradePrice="150.5" levelOfDetail="LOT" />
</Trades>
<OpenPositions>
<OpenPosition symbol="AAPL" conid="265598" position="10" markPrice="160" positionValue="1600" fifoPnlUnrealized="95" />
</OpenPositions>
<OptionEAE>
<OptionEAE symbol="SPY" transactionType="Assignment" quantity="1" tradePrice="0" realizedPnl="0" />
<OptionEAE symbol="SPY" transactionType="Exercise" quantity="-1" />
</OptionEAE>
<FxPositions>
<FxPosition fxCurrency="EUR" quantity="100" value="108" unrealizedPL="3" />
<FxLots>
<FxLot fxCurrency="EUR" quantity="100" value="108" />
</FxLots>
</FxPositions>
<CorporateActions>
<CorporateAction symbol="VOO" type="Merger" amount="42" />
</CorporateActions>
</FlexStatement>
</FlexStatements>
</FlexQueryResponse>`)

func TestFullSectionsParsing(t *testing.T) {
	var r Report
	if err := xml.Unmarshal(fullSectionsXML, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(r.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(r.Statements))
	}
	s := r.Statements[0]

	// Shared Trade shape, and empty strike decodes to 0.
	if len(s.Trades.Trade) != 1 || len(s.Trades.Lot) != 1 {
		t.Fatalf("trades: got Trade=%d Lot=%d", len(s.Trades.Trade), len(s.Trades.Lot))
	}
	tr := s.Trades.Trade[0]
	if tr.Quantity != 10 || tr.TradePrice != 150.5 || tr.Proceeds != -1505 {
		t.Errorf("trade numbers: qty=%v price=%v proceeds=%v", tr.Quantity, tr.TradePrice, tr.Proceeds)
	}
	if tr.Strike != 0 {
		t.Errorf("empty strike should decode to 0, got %v", tr.Strike)
	}
	if tr.Conid != "265598" || tr.BuySell != "BUY" {
		t.Errorf("trade strings: conid=%q buySell=%q", tr.Conid, tr.BuySell)
	}

	// Open positions.
	if len(s.OpenPositions.OpenPosition) != 1 {
		t.Fatalf("expected 1 open position, got %d", len(s.OpenPositions.OpenPosition))
	}
	if s.OpenPositions.OpenPosition[0].PositionValue != 1600 {
		t.Errorf("position value: %v", s.OpenPositions.OpenPosition[0].PositionValue)
	}

	// Self-nesting OptionEAE: rows reached through the nested path.
	if len(s.OptionEAE) != 2 {
		t.Fatalf("expected 2 OptionEAE rows, got %d", len(s.OptionEAE))
	}
	if s.OptionEAE[0].TransactionType != "Assignment" {
		t.Errorf("optionEAE[0] transactionType=%q", s.OptionEAE[0].TransactionType)
	}

	// FX positions and the lots slice.
	if len(s.FxPositions.FxPosition) != 1 || len(s.FxPositions.FxLots.FxLot) != 1 {
		t.Fatalf("fx: positions=%d lots=%d", len(s.FxPositions.FxPosition), len(s.FxPositions.FxLots.FxLot))
	}

	// Empty-in-sample section still captures rows via RawElement.
	if len(s.CorporateActions.Unmodeled) != 1 {
		t.Fatalf("expected 1 raw corporate action, got %d", len(s.CorporateActions.Unmodeled))
	}
	raw := s.CorporateActions.Unmodeled[0]
	if raw.XMLName.Local != "CorporateAction" {
		t.Errorf("raw element name=%q", raw.XMLName.Local)
	}
	var gotType string
	for _, a := range raw.Attrs {
		if a.Name.Local == "type" {
			gotType = a.Value
		}
	}
	if gotType != "Merger" {
		t.Errorf("raw corporate action type=%q, want Merger", gotType)
	}
}

func TestAccountInformationPostalCodesStayStrings(t *testing.T) {
	const accountXML = `<FlexQueryResponse queryName="account" type="AF">
<FlexStatements count="1">
<FlexStatement accountId="U1">
<AccountInformation postalCode="94103" postalCodeResidentialAddress="SW1A 1AA" />
</FlexStatement>
</FlexStatements>
</FlexQueryResponse>`

	var r Report
	if err := xml.Unmarshal([]byte(accountXML), &r); err != nil {
		t.Fatalf("unmarshal account information: %v", err)
	}
	if len(r.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(r.Statements))
	}
	info := r.Statements[0].AccountInformation
	if len(info) != 1 {
		t.Fatalf("expected 1 account information row, got %d", len(info))
	}
	if info[0].PostalCodeResidentialAddress != "SW1A 1AA" {
		t.Errorf("postalCodeResidentialAddress=%q, want SW1A 1AA", info[0].PostalCodeResidentialAddress)
	}
}

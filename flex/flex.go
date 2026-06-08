// Package flex is a client for the Interactive Brokers Flex Web Service, a
// separate IBKR API from the Client Portal Gateway. It downloads Activity Flex
// Query reports directly from IBKR over HTTPS using a token and query ID, with
// no Java gateway, browser login, or session to keep alive. Reports are XML
// (not JSON like the Client Portal endpoints).
//
// The Cash Transactions section of an Activity Flex Query reports the cash
// flows the retail Client Portal does not expose: deposits, withdrawals, fees,
// dividends, and interest. The full Activity schema (trades, positions, and so
// on) is modeled in flex_sections.go.
//
// https://www.interactivebrokers.com/campus/ibkr-api-page/flex-web-service/
//
// Setup, done once in Account Management:
//
//  1. Reports > Settings > Flex Web Service: enable it and generate a token.
//  2. Reports > Flex Queries > Custom Activity Flex Query: build a query that
//     includes the sections you want, and note its Query ID.
//
// Download is a two-step process: SendRequest exchanges a query ID for a
// reference code, then GetStatement exchanges the reference code for the
// generated report. Download performs both steps and polls while the report
// is still generating.
package flex

// flex_sections.go is generated from testdata/sample.xml, a synthetic,
// schema-complete sample report (no real account data). To refresh the schema
// when IBKR adds columns, replace testdata/sample.xml with a freshly sanitized
// report and run go generate. gen.py requires python3.
//go:generate python3 gen.py
//go:generate gofmt -w flex_sections.go

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/kevinburke/ibclientportal"
	"github.com/kevinburke/rest/restclient"
)

const (
	// RequestURL is the default SendRequest endpoint (step one).
	RequestURL = "https://ndcdyn.interactivebrokers.com/AccountManagement/FlexWebService/SendRequest"
	// StatementURL is the default GetStatement endpoint (step two).
	StatementURL = "https://gdcdyn.interactivebrokers.com/AccountManagement/FlexWebService/GetStatement"
)

// apiVersion is the Flex Web Service protocol version. Version 3 is the only
// version that is still supported.
const apiVersion = "3"

// defaultMaxTries bounds how many times Download polls GetStatement while the
// report is still generating. With the 5 second retry delay this is roughly a
// one minute ceiling.
const defaultMaxTries = 12

// Retry delays returned by the Flex server, mirroring the codes it documents.
const (
	defaultRetryDelay    = 5 * time.Second
	defaultThrottleDelay = 10 * time.Second
)

// CashAction values for the Type field of a CashTransaction. These are the
// human-readable strings IBKR emits in the report XML, confirmed against live
// Activity Flex Query output.
const (
	// CashActionDepositsWithdrawals is the type for deposits and withdrawals.
	// Live reports emit "Deposits/Withdrawals" (with a slash); some older
	// documentation and libraries show "Deposits & Withdrawals" instead.
	CashActionDepositsWithdrawals    = "Deposits/Withdrawals"
	CashActionBrokerInterestPaid     = "Broker Interest Paid"
	CashActionBrokerInterestReceived = "Broker Interest Received"
	CashActionWithholdingTax         = "Withholding Tax"
	CashActionBondInterestReceived   = "Bond Interest Received"
	CashActionBondInterestPaid       = "Bond Interest Paid"
	CashActionOtherFees              = "Other Fees"
	CashActionDividends              = "Dividends"
	CashActionPaymentInLieu          = "Payment In Lieu Of Dividends"
	CashActionCommissionAdjustments  = "Commission Adjustments"
	CashActionAdvisorFees            = "Advisor Fees"
)

// Client downloads Flex Web Service reports. The zero value is usable; fields
// default lazily. Construct one with NewClient to set the token up front.
type Client struct {
	// Client is the HTTP client used for requests. Defaults to an
	// http.Client using restclient.DefaultTransport.
	Client *http.Client
	// Token is the Flex Web Service token from Reports > Settings.
	Token string
	// UserAgent is sent on every request. Defaults to the module user agent.
	UserAgent string
	// MaxTries bounds how many times Download polls for a generating report.
	MaxTries int

	requestURL    string
	statementURL  string
	retryDelay    time.Duration
	throttleDelay time.Duration
}

// NewClient returns a Client that authenticates with the given Flex Web Service
// token.
func NewClient(token string) *Client {
	return &Client{
		Client:        &http.Client{Transport: restclient.DefaultTransport},
		Token:         token,
		UserAgent:     ibclientportal.UserAgent,
		MaxTries:      defaultMaxTries,
		requestURL:    RequestURL,
		statementURL:  StatementURL,
		retryDelay:    defaultRetryDelay,
		throttleDelay: defaultThrottleDelay,
	}
}

func (c *Client) ensureDefaults() {
	if c.Client == nil {
		c.Client = &http.Client{Transport: restclient.DefaultTransport}
	}
	if c.UserAgent == "" {
		c.UserAgent = ibclientportal.UserAgent
	}
	if c.MaxTries <= 0 {
		c.MaxTries = defaultMaxTries
	}
	if c.requestURL == "" {
		c.requestURL = RequestURL
	}
	if c.statementURL == "" {
		c.statementURL = StatementURL
	}
	if c.retryDelay == 0 {
		c.retryDelay = defaultRetryDelay
	}
	if c.throttleDelay == 0 {
		c.throttleDelay = defaultThrottleDelay
	}
}

// Error is returned when the Flex server reports an error code, including the
// transient "statement generation in progress" code that Download retries on.
//
// https://www.ibkrguides.com/clientportal/performanceandstatements/flex3error.htm
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("flex: code %s: %s", e.Code, e.Message)
}

// Retryable reports whether the error is transient and the same request may
// succeed if retried after a short delay.
func (e *Error) Retryable() bool {
	return e.retrySoon() || e.throttled()
}

// retrySoon reports codes IBKR documents with "try again shortly" style
// guidance. These cover reports still generating, backend data not ready,
// temporary retrieval failures, and server load.
func (e *Error) retrySoon() bool {
	switch e.Code {
	case "1001", "1004", "1005", "1006", "1007", "1008", "1009", "1019", "1021":
		return true
	default:
		return false
	}
}

// throttled reports the "too many requests from this token" code, which needs a
// longer pause before retrying.
func (e *Error) throttled() bool {
	return e.Code == "1018"
}

// BadResponseError is returned when the Flex server returns a body that is
// neither a recognized report nor a recognized error. The raw body is included
// so failures are visible rather than silent. IBKR will, for example, return an
// HTML page rather than XML when a request is blocked, so surfacing the body is
// important for diagnosis.
type BadResponseError struct {
	Body []byte
	Err  error
}

func (e *BadResponseError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("flex: could not parse response: %v: body=%q", e.Err, truncateBody(e.Body))
	}
	return fmt.Sprintf("flex: unexpected response: body=%q", truncateBody(e.Body))
}

func (e *BadResponseError) Unwrap() error { return e.Err }

func truncateBody(b []byte) string {
	const max = 512
	if len(b) > max {
		return string(b[:max]) + "..."
	}
	return string(b)
}

// statementResponse is the envelope returned by SendRequest, and by
// GetStatement when the report is not ready or an error occurred.
type statementResponse struct {
	XMLName       xml.Name `xml:"FlexStatementResponse"`
	Timestamp     string   `xml:"timestamp,attr"`
	Status        string   `xml:"Status"`
	ReferenceCode string   `xml:"ReferenceCode"`
	URL           string   `xml:"Url"`
	ErrorCode     string   `xml:"ErrorCode"`
	ErrorMessage  string   `xml:"ErrorMessage"`
}

// Report is a parsed Activity Flex Query report. Statement and the other
// section types are defined in flex_sections.go.
type Report struct {
	XMLName    xml.Name    `xml:"FlexQueryResponse"`
	QueryName  string      `xml:"queryName,attr"`
	Type       string      `xml:"type,attr"`
	Statements []Statement `xml:"FlexStatements>FlexStatement"`
}

// CashTransactions returns the cash transactions from every statement in the
// report, flattened into a single slice.
func (r *Report) CashTransactions() []CashTransaction {
	var out []CashTransaction
	for i := range r.Statements {
		out = append(out, r.Statements[i].CashTransactions.CashTransaction...)
	}
	return out
}

// Float is a numeric Flex report attribute. IBKR emits an empty string for
// numeric columns that do not apply to a given row (for example strike on a
// stock trade), which the standard float64 attribute unmarshaler rejects; Float
// decodes an empty attribute as 0.
type Float float64

// UnmarshalXMLAttr implements xml.UnmarshalerAttr.
func (f *Float) UnmarshalXMLAttr(attr xml.Attr) error {
	if attr.Value == "" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(attr.Value, 64)
	if err != nil {
		return fmt.Errorf("flex: parsing %q as a number: %w", attr.Value, err)
	}
	*f = Float(v)
	return nil
}

// RawElement captures an XML element that is not otherwise modeled, preserving
// its name and attributes so that data is never silently dropped. It is used
// for report sections that were empty in the sample the model was built from.
type RawElement struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
}

// Download runs the full two-step download for the given Flex Query ID: it
// requests the report, then polls until the report is generated and returns
// it. It retries on the transient "try again shortly" and throttled codes,
// respecting the supplied context for cancellation and deadlines.
func (c *Client) Download(ctx context.Context, queryID string) (*Report, error) {
	c.ensureDefaults()
	referenceCode, err := c.SendRequest(ctx, queryID)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for try := 0; try < c.MaxTries; try++ {
		resp, err := c.GetStatement(ctx, referenceCode)
		if err == nil {
			return resp, nil
		}
		var ferr *Error
		if errors.As(err, &ferr) {
			delay := time.Duration(0)
			switch {
			case ferr.throttled():
				delay = c.throttleDelay
			case ferr.retrySoon():
				delay = c.retryDelay
			}
			if delay > 0 {
				lastErr = err
				if sleepErr := sleepContext(ctx, delay); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
		}
		return nil, err
	}
	return nil, fmt.Errorf("flex: report did not finish generating after %d tries: %w", c.MaxTries, lastErr)
}

// SendRequest performs step one of the download: it submits a Flex Query ID and
// returns the reference code used to fetch the generated report.
func (c *Client) SendRequest(ctx context.Context, queryID string) (string, error) {
	c.ensureDefaults()
	body, err := c.get(ctx, c.requestURL, queryID)
	if err != nil {
		return "", err
	}
	var sr statementResponse
	if err := xml.Unmarshal(body, &sr); err != nil {
		return "", &BadResponseError{Body: body, Err: err}
	}
	if sr.Status != "Success" {
		return "", &Error{Code: sr.ErrorCode, Message: sr.ErrorMessage}
	}
	if sr.ReferenceCode == "" {
		return "", &BadResponseError{Body: body, Err: errors.New("missing ReferenceCode in successful response")}
	}
	return sr.ReferenceCode, nil
}

// GetStatement performs step two of the download: it exchanges a reference code
// for the generated report. While the report is still generating the server
// returns an error envelope; GetStatement returns that as an *Error whose
// Retryable method reports true.
func (c *Client) GetStatement(ctx context.Context, referenceCode string) (*Report, error) {
	c.ensureDefaults()
	body, err := c.get(ctx, c.statementURL, referenceCode)
	if err != nil {
		return nil, err
	}
	root, err := rootElement(body)
	if err != nil {
		return nil, &BadResponseError{Body: body, Err: err}
	}
	switch root {
	case "FlexQueryResponse":
		var report Report
		if err := xml.Unmarshal(body, &report); err != nil {
			return nil, &BadResponseError{Body: body, Err: err}
		}
		return &report, nil
	case "FlexStatementResponse":
		var sr statementResponse
		if err := xml.Unmarshal(body, &sr); err != nil {
			return nil, &BadResponseError{Body: body, Err: err}
		}
		return nil, &Error{Code: sr.ErrorCode, Message: sr.ErrorMessage}
	default:
		return nil, &BadResponseError{Body: body}
	}
}

// get issues a Flex Web Service GET request and returns the response body. The
// Flex service expects the token, query and version as query parameters.
//
// IBKR documents both SendRequest and GetStatement as GET requests with t
// (token), q (query ID or reference code), and v=3:
// https://www.interactivebrokers.com/campus/ibkr-api-page/flex-web-service/
func (c *Client) get(ctx context.Context, base, query string) ([]byte, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("flex: invalid URL %q: %w", base, err)
	}
	q := u.Query()
	q.Set("v", apiVersion)
	q.Set("t", c.Token)
	q.Set("q", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("flex: reading response body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("flex: unexpected HTTP status %s: body=%q", resp.Status, truncateBody(body))
	}
	return body, nil
}

// rootElement returns the local name of the first XML element in body.
func rootElement(body []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se.Name.Local, nil
		}
	}
}

// sleepContext sleeps for d, returning early with the context's error if the
// context is cancelled first.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

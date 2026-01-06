package ibclientportal

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"time"

	"github.com/kevinburke/rest/restclient"
)

// Client interacts with the Interactive Brokers Client Portal API.
// https://www.interactivebrokers.com/api/doc.html#tag/Contract/paths/~1trsrv~1futures/get
type Client struct {
	*restclient.Client
	insecureSkipVerify bool

	Contracts           *ContractService
	MarketData          *MarketDataService
	Orders              *OrdersService
	Portfolio           *PortfolioService
	SecurityDefinitions *SecurityDefinitionService
}

// The ibclientportal version. Run "make release" to bump this number.
const Version = "0.1.0"

const userAgent = "ibclientportal-go/" + Version

func (c *Client) MakeRequest(ctx context.Context, method string, pathPart string, data url.Values, requestBody interface{}, resp interface{}) error {
	if c == nil {
		panic("nil client")
	}
	var rb io.Reader = nil
	if requestBody != nil && (method == "POST" || method == "PUT") {
		jsonData, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		rb = bytes.NewReader(jsonData)
	}
	if method == "GET" && data != nil {
		pathPart = pathPart + "?" + data.Encode()
	}
	req, err := c.NewRequestWithContext(ctx, method, pathPart, rb)
	if err != nil {
		return err
	}
	if ua := req.Header.Get("User-Agent"); ua == "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", userAgent+" "+ua)
	}
	return c.Do(req, &resp)
}

func (c *Client) ListResource(ctx context.Context, pathPart string, data url.Values, v interface{}) error {
	return c.MakeRequest(ctx, "GET", pathPart, data, nil, v)
}

func (c *Client) UpdateResource(ctx context.Context, pathPart string, data interface{}, resp interface{}) error {
	return c.MakeRequest(ctx, "POST", pathPart, url.Values{}, data, resp)
}

type TickleResponse struct {
	Session    string        `json:"session"`
	SSOExpires int64         `json:"ssoExpires"`
	Collision  bool          `json:"collission"` // sic!
	UserID     int64         `json:"userId"`
	IServer    TickleIServer `json:"iServer"`
}

type TickleIServer struct {
	AuthStatus TickleAuthStatus `json:"authStatus"`
}

type TickleAuthStatus struct {
	Authenticated bool   `json:"authenticated"`
	Competing     bool   `json:"competing"`
	Connected     bool   `json:"connected"`
	Message       string `json:"message"`
	MAC           string `json:"MAC"`
}

func (c *Client) Tickle(ctx context.Context, data url.Values) (TickleResponse, error) {
	path := "/tickle"
	var val TickleResponse
	err := c.UpdateResource(ctx, path, data, &val)
	return val, err
}

// AuthStatusResponse is the response from /iserver/auth/status.
type AuthStatusResponse struct {
	Authenticated bool              `json:"authenticated"`
	Competing     bool              `json:"competing"`
	Connected     bool              `json:"connected"`
	Message       string            `json:"message"`
	MAC           string            `json:"MAC"`
	ServerInfo    map[string]string `json:"serverInfo"`
	HardwareInfo  string            `json:"hardware_info"`
	Fail          string            `json:"fail"`
}

// AuthStatus returns the current brokerage session authentication status.
// Market Data and Trading is not possible if not authenticated.
func (c *Client) AuthStatus(ctx context.Context) (AuthStatusResponse, error) {
	path := "/iserver/auth/status"
	var val AuthStatusResponse
	err := c.UpdateResource(ctx, path, nil, &val)
	return val, err
}

type ContractService struct {
	client *Client
}

type ContractStocksResponse map[string][]ContractStock

type ContractStock struct {
	Name        string     `json:"name"`
	ChineseName string     `json:"chineseName"`
	AssetClass  string     `json:"assetClass"`
	Contracts   []Contract `json:"contracts"`
}

type Contract struct {
	ContractID int64  `json:"conid"`
	Exchange   string `json:"exchange"`
	IsUS       bool   `json:"isUS"`
}

func (c *ContractService) Stocks(ctx context.Context, query url.Values) (ContractStocksResponse, error) {
	path := "/trsrv/stocks"
	var val ContractStocksResponse
	err := c.client.ListResource(ctx, path, query, &val)
	return val, err
}

type SecurityDefinitionService struct {
	client *Client
}

type SecurityDefinitionSearchParameters struct {
	// symbol or name to be searched
	Symbol string `json:"symbol,omitempty"`
	// should be true if the search is to be performed by name. false by default.
	Name bool `json:"name"`
	// If search is done by name, only the assets provided in this field will be returned. Currently, only STK is supported.
	SecType string `json:"secType,omitempty"`
}

type SecurityDefinitionSearchResponse []SecurityDefinitionSearchElement

type SecurityDefinitionSearchElement struct {
	ContractID int64 `json:"conid"`

	CompanyName string `json:"companyName"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`

	// more fields...
}

type securityDefinitionSearchElement struct {
	ContractID string `json:"conid"`

	CompanyName string `json:"companyName"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`

	// more fields...
}

func (s *SecurityDefinitionSearchElement) UnmarshalJSON(p []byte) error {
	se := new(securityDefinitionSearchElement)
	if err := json.Unmarshal(p, se); err != nil {
		return err
	}
	conid, err := strconv.ParseInt(se.ContractID, 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse ContractID %q as int64: %v", se.ContractID, err)
	}
	s.ContractID = conid
	s.CompanyName = se.CompanyName
	s.Symbol = se.Symbol
	s.Description = se.Description
	return nil
}

func (c *SecurityDefinitionService) Search(ctx context.Context, query SecurityDefinitionSearchParameters) (SecurityDefinitionSearchResponse, error) {
	path := "/iserver/secdef/search"
	var val SecurityDefinitionSearchResponse
	err := c.client.UpdateResource(ctx, path, query, &val)
	return val, err
}

type MarketDataService struct {
	client *Client
}

func (m *MarketDataService) History(ctx context.Context, query url.Values) (*MarketDataHistoryResponse, error) {
	path := "/iserver/marketdata/history"
	var val MarketDataHistoryResponse
	err := m.client.ListResource(ctx, path, query, &val)
	return &val, err
}

type MarketDataHistoryResponse struct {
	Symbol     string                  `json:"symbol"`
	Text       string                  `json:"text"`
	TimePeriod string                  `json:"timePeriod"`
	Data       []MarketDataHistoryData `json:"data"`
}

type MarketDataHistoryData struct {
	Open   float64
	Close  float64
	High   float64
	Low    float64
	Volume float64
	Time   time.Time
}

type marketDataHistoryData struct {
	Open   float64 `json:"o"`
	Close  float64 `json:"c"`
	High   float64 `json:"h"`
	Low    float64 `json:"l"`
	Volume float64 `json:"v"`
	// TODO: convert this to a native format.
	// TODO: this is a "timeless" unit.
	TimestampMillis int64 `json:"t"`
}

func (m *MarketDataHistoryData) UnmarshalJSON(p []byte) error {
	se := new(marketDataHistoryData)
	if err := json.Unmarshal(p, se); err != nil {
		return err
	}
	m.Open = se.Open
	m.Close = se.Close
	m.High = se.High
	m.Low = se.Low
	m.Volume = se.Volume
	m.Time = time.UnixMilli(se.TimestampMillis)
	return nil
}

type Day struct {
	Year  int
	Month time.Month
	Day   int
}

func (m *MarketDataHistoryData) Day() Day {
	return Day{m.Time.Year(), m.Time.Month(), m.Time.Day()}
}

type PortfolioService struct {
	client *Client
}

// AccountParent contains information about the parent account relationship.
type AccountParent struct {
	MMC         []string `json:"mmc"`
	AccountID   string   `json:"accountId"`
	IsMParent   bool     `json:"isMParent"`
	IsMChild    bool     `json:"isMChild"`
	IsMultiplex bool     `json:"isMultiplex"`
}

// Account represents an Interactive Brokers account.
type Account struct {
	ID                      string        `json:"id"`
	AccountID               string        `json:"accountId"`
	AccountVan              string        `json:"accountVan"`
	AccountTitle            string        `json:"accountTitle"`
	DisplayName             string        `json:"displayName"`
	AccountAlias            *string       `json:"accountAlias"`
	AccountStatus           int64         `json:"accountStatus"`
	Currency                string        `json:"currency"`
	Type                    string        `json:"type"`
	TradingType             string        `json:"tradingType"`
	BusinessType            string        `json:"businessType"`
	IBEntity                string        `json:"ibEntity"`
	FAClient                bool          `json:"faclient"`
	ClearingStatus          string        `json:"clearingStatus"`
	Covestor                bool          `json:"covestor"`
	NoClientTrading         bool          `json:"noClientTrading"`
	TrackVirtualFXPortfolio bool          `json:"trackVirtualFXPortfolio"`
	Parent                  AccountParent `json:"parent"`
	Desc                    string        `json:"desc"`
	BrokerageAccess         bool          `json:"brokerageAccess"`
}

// ListAccounts returns all accounts associated with the current session.
func (p *PortfolioService) ListAccounts(ctx context.Context) ([]Account, error) {
	path := "/portfolio/accounts"
	var val []Account
	err := p.client.ListResource(ctx, path, nil, &val)
	return val, err
}

// Position represents a position in an Interactive Brokers account.
type Position struct {
	Position      float64 `json:"position"`
	ContractID    int64   `json:"conid,string"`
	AvgCost       float64 `json:"avgCost"`
	AvgPrice      float64 `json:"avgPrice"`
	Currency      string  `json:"currency"`
	Description   string  `json:"description"`
	IsLastToLiq   bool    `json:"isLastToLoq"`
	MarketPrice   float64 `json:"marketPrice"`
	MarketValue   float64 `json:"marketValue"`
	RealizedPnL   float64 `json:"realizedPnl"`
	UnrealizedPnL float64 `json:"unrealizedPnl"`
	SecType       string  `json:"secType"`
	Timestamp     int64   `json:"timestamp"`
	AssetClass    string  `json:"assetClass"`
	Sector        string  `json:"sector"`
	Group         string  `json:"group"`
	Model         string  `json:"model"`
}

// ListPositions returns positions for the given account.
// ListAccounts must be called prior to this endpoint.
// Query parameters: model, sort, direction (a=ascending, d=descending).
func (p *PortfolioService) ListPositions(ctx context.Context, accountID string, query url.Values) ([]Position, error) {
	path := "/portfolio2/" + accountID + "/positions"
	var val []Position
	err := p.client.ListResource(ctx, path, query, &val)
	return val, err
}

type OrdersService struct {
	client *Client
}

// TradableAccountsResponse is the response from /iserver/accounts.
type TradableAccountsResponse struct {
	Accounts        []string               `json:"accounts"`
	AcctProps       map[string]interface{} `json:"acctProps"`
	Aliases         map[string]string      `json:"aliases"`
	SelectedAccount string                 `json:"selectedAccount"`
	IsFT            bool                   `json:"isFt"`
	IsPaper         bool                   `json:"isPaper"`
}

// ListTradableAccounts returns a list of accounts the user has trading access to.
// Note: this endpoint must be called before modifying an order or querying open orders/trades.
func (o *OrdersService) ListTradableAccounts(ctx context.Context) (TradableAccountsResponse, error) {
	path := "/iserver/accounts"
	var val TradableAccountsResponse
	err := o.client.ListResource(ctx, path, nil, &val)
	return val, err
}

// Order represents a live order in an Interactive Brokers account.
type Order struct {
	Account            string  `json:"acct"`
	AccountID          string  `json:"account"`
	ConIDEx            string  `json:"conidex"`
	ContractID         int64   `json:"conid"`
	OrderID            int64   `json:"orderId"`
	CashCurrency       string  `json:"cashCcy"`
	SizeAndFills       string  `json:"sizeAndFills"`
	OrderDesc          string  `json:"orderDesc"`
	Description1       string  `json:"description1"`
	Ticker             string  `json:"ticker"`
	SecType            string  `json:"secType"`
	ListingExchange    string  `json:"listingExchange"`
	RemainingQuantity  float64 `json:"remainingQuantity"`
	FilledQuantity     float64 `json:"filledQuantity"`
	TotalSize          float64 `json:"totalSize"`
	CompanyName        string  `json:"companyName"`
	Status             string  `json:"status"`
	OrderCCPStatus     string  `json:"order_ccp_status"`
	AvgPrice           string  `json:"avgPrice"`
	OrigOrderType      string  `json:"origOrderType"`
	SupportsTaxOpt     string  `json:"supportsTaxOpt"`
	LastExecutionTime  string  `json:"lastExecutionTime"`
	OrderType          string  `json:"orderType"`
	BGColor            string  `json:"bgColor"`
	FGColor            string  `json:"fgColor"`
	OrderRef           string  `json:"order_ref"`
	TimeInForce        string  `json:"timeInForce"`
	LastExecutionTimeR int64   `json:"lastExecutionTime_r"`
	Side               string  `json:"side"`
}

// OrdersResponse is the response from the orders endpoint.
type OrdersResponse struct {
	Orders   []Order `json:"orders"`
	Snapshot bool    `json:"snapshot"`
}

// SwitchAccountResponse is the response from switching accounts.
type SwitchAccountResponse struct {
	Set       bool   `json:"set"`
	AccountID string `json:"acctId"`
}

// SwitchAccount switches the active account for the session.
// This must be called before certain endpoints like ListOrders.
func (o *OrdersService) SwitchAccount(ctx context.Context, accountID string) (SwitchAccountResponse, error) {
	// Clear existing session cookies to prevent accumulation
	// (the API sets a new x-sess-uuid on each response)
	o.client.clearSessionCookies()

	path := "/iserver/account"
	body := struct {
		AccountID string `json:"acctId"`
	}{AccountID: accountID}
	var val SwitchAccountResponse
	err := o.client.UpdateResource(ctx, path, body, &val)
	return val, err
}

// ListOrders returns live orders for the current session.
// SwitchAccount should be called first to select the appropriate account.
// Query parameters: filters (comma-separated status values), force (bool).
func (o *OrdersService) ListOrders(ctx context.Context, query url.Values) (OrdersResponse, error) {
	path := "/iserver/account/orders"
	var val OrdersResponse
	err := o.client.ListResource(ctx, path, query, &val)
	return val, err
}

// ListOrdersForAccount switches to the specified account and then returns its live orders.
// This is a convenience method that calls SwitchAccount followed by ListOrders.
func (o *OrdersService) ListOrdersForAccount(ctx context.Context, accountID string, query url.Values) (OrdersResponse, error) {
	_, err := o.SwitchAccount(ctx, accountID)
	if err != nil {
		return OrdersResponse{}, fmt.Errorf("switching to account %s: %w", accountID, err)
	}
	return o.ListOrders(ctx, query)
}

// Trade represents a completed trade/execution in an Interactive Brokers account.
type Trade struct {
	ExecutionID           string  `json:"execution_id"`
	Symbol                string  `json:"symbol"`
	SupportsTaxOpt        string  `json:"supports_tax_opt"`
	Side                  string  `json:"side"` // "B" or "S"
	OrderDescription      string  `json:"order_description"`
	TradeTime             string  `json:"trade_time"` // YYYYMMDD-hh:mm:ss UTC
	TradeTimeR            int64   `json:"trade_time_r"`
	Size                  float64 `json:"size"`
	Price                 string  `json:"price"`
	OrderRef              string  `json:"order_ref"`
	Submitter             string  `json:"submitter"`
	Exchange              string  `json:"exchange"`
	Commission            string  `json:"commission"`
	NetAmount             float64 `json:"net_amount"`
	Account               string  `json:"account"`
	AccountCode           string  `json:"accountCode"`
	AccountAllocationName string  `json:"account_allocation_name"`
	CompanyName           string  `json:"company_name"`
	ContractDescription1  string  `json:"contract_description_1"`
	SecType               string  `json:"sec_type"`
	ListingExchange       string  `json:"listing_exchange"`
	ContractID            int64   `json:"conid"`
	ContractIDEx          string  `json:"conidEx"`
	ClearingID            string  `json:"clearing_id"`
	ClearingName          string  `json:"clearing_name"`
	LiquidationTrade      string  `json:"liquidation_trade"`
	IsEventTrading        string  `json:"is_event_trading"`
	OrderID               float64 `json:"order_id"`
}

// ListTrades returns trades/executions for the current session.
// Query parameters: days (int, 1-7, default 1 for current day only).
func (o *OrdersService) ListTrades(ctx context.Context, query url.Values) ([]Trade, error) {
	// Clear session cookies - the trades endpoint returns empty results
	// if stale session cookies from other API calls are present
	o.client.clearSessionCookies()

	path := "/iserver/account/trades"
	var val []Trade
	err := o.client.ListResource(ctx, path, query, &val)
	return val, err
}

const DefaultHost = "https://localhost:5000"

func New(host string) *Client {
	if host == "" {
		host = DefaultHost
	}
	rc := restclient.New("", "", host+"/v1/api")
	rc.UploadType = restclient.JSON

	// Create a cookie jar to persist session cookies across requests
	// (required for account switching to work properly)
	jar, _ := cookiejar.New(nil)
	rc.Client.Jar = jar

	c := &Client{
		Client: rc,
	}

	c.Contracts = &ContractService{c}
	c.MarketData = &MarketDataService{c}
	c.Orders = &OrdersService{c}
	c.Portfolio = &PortfolioService{c}
	c.SecurityDefinitions = &SecurityDefinitionService{c}
	return c
}

func setInsecure(tr *http.Transport) {
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
}

// clearSessionCookies replaces the cookie jar with a fresh one to prevent
// accumulation of session cookies across requests.
func (c *Client) clearSessionCookies() {
	jar, _ := cookiejar.New(nil)
	c.Client.Client.Jar = jar
}

func (c *Client) SetInsecureSkipVerify() {
	ctr := c.Client.Client.Transport
	tr, ok := ctr.(*http.Transport)
	if ok {
		setInsecure(tr)
		return
	}
	rct, ok := ctr.(*restclient.Transport)
	if !ok {
		panic(fmt.Sprintf("don't know how to set insecure skip verify on this http.RoundTripper: %#v", ctr))
	}
	tr, ok = rct.RoundTripper.(*http.Transport)
	if !ok {
		panic(fmt.Sprintf("unknown transport set on restclient.Transport: %#v", rct.RoundTripper))
	}
	setInsecure(tr)
}

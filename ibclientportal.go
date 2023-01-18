package ibclientportal

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/kevinburke/rest/restclient"
)

// Client interacts with the Interactive Brokers Client Portal API.
// https://www.interactivebrokers.com/api/doc.html#tag/Contract/paths/~1trsrv~1futures/get
type Client struct {
	*restclient.Client
	insecureSkipVerify bool

	SecurityDefinitions *SecurityDefinitionService
	Contracts           *ContractService
	MarketData          *MarketDataService
}

// The ibclientportal version. Run "make release" to bump this number.
const Version = "0.1"

const userAgent = "buildkite-go/" + Version

func (c *Client) MakeRequest(ctx context.Context, method string, pathPart string, data url.Values, requestBody interface{}, resp interface{}) error {
	var rb *bytes.Reader
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
	ContractID  int64  `json:"conid"`
	CompanyName string `json:"companyName"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`

	// more fields...
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
	Open   float64 `json:"o"`
	Close  float64 `json:"c"`
	High   float64 `json:"h"`
	Low    float64 `json:"l"`
	Volume int64   `json:"v"`
	// TODO: convert this to a native format.
	// TODO: this is a "timeless" unit.
	TimestampMillis int64 `json:"t"`

	time time.Time
}

type Day struct {
	Year  int
	Month time.Month
	Day   int
}

func (m *MarketDataHistoryData) Day() Day {
	m.fillTime()
	return Day{m.time.Year(), m.time.Month(), m.time.Day()}
}

func (m *MarketDataHistoryData) fillTime() {
	if m.time.IsZero() {
		m.time = time.UnixMilli(m.TimestampMillis)
	}
}

func (m *MarketDataHistoryData) Time() time.Time {
	m.fillTime()
	return m.time
}

const DefaultHost = "https://localhost:5000"

func New(host string) *Client {
	if host == "" {
		host = DefaultHost
	}
	rc := restclient.New("", "", DefaultHost+"/v1/api")
	rc.UploadType = restclient.JSON
	c := &Client{
		Client: rc,
	}

	c.Contracts = &ContractService{c}
	c.MarketData = &MarketDataService{c}
	return c
}

func setInsecure(tr *http.Transport) {
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
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

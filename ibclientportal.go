package ibclientportal

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/kevinburke/rest/restclient"
)

// Client interacts with the Interactive Brokers Client Portal API.
// https://www.interactivebrokers.com/api/doc.html#tag/Contract/paths/~1trsrv~1futures/get
type Client struct {
	*restclient.Client
	insecureSkipVerify bool

	Contracts *ContractService
}

// The ibclientportal version. Run "make release" to bump this number.
const Version = "0.1"

const userAgent = "buildkite-go/" + Version

func (c *Client) MakeRequest(ctx context.Context, method string, pathPart string, data url.Values, v interface{}) error {
	rb := new(strings.Reader)
	if data != nil && (method == "POST" || method == "PUT") {
		rb = strings.NewReader(data.Encode())
	}
	if method == "GET" && data != nil {
		pathPart = pathPart + "?" + data.Encode()
	}
	req, err := c.NewRequest(method, pathPart, rb)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	if ua := req.Header.Get("User-Agent"); ua == "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", userAgent+" "+ua)
	}
	return c.Do(req, &v)
}

func (c *Client) ListResource(ctx context.Context, pathPart string, data url.Values, v interface{}) error {
	return c.MakeRequest(ctx, "GET", pathPart, data, v)
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

const DefaultHost = "https://localhost:5001"

func New(host string) *Client {
	if host == "" {
		host = DefaultHost
	}
	rc := restclient.New("", "", DefaultHost+"/v1/api")
	c := &Client{
		Client: rc,
	}

	c.Contracts = &ContractService{c}
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

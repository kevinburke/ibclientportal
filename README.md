# IB Client Portal

This is a partial API designed to work with the Interactive Brokers Client
Portal API.

To get started you want to download the Interactive Brokers "gw" tool, a Java
app, that handles authentication for you. Then you can make requests against
that API.

```
cd /path/to/clientportal.gw
./bin/run.sh ./root/conf.yaml

running
 runtime path : root:dist/ibgroup.web.core.iblink.router.clientportal.gw.jar:build/lib/runtime/*
 verticle     :
 -> mount demo on /demo
Java Version: 19.0.2
****************************************************
version: ed4af2592e9dd4a784d5403843bd18292fd441ea Fri, 9 Nov 2018 13:23:18 -0500
****************************************************
This is a Beta release of the Client Portal Gateway
for any issues, please contact api@ibkr.com
and include a copy of your logs
****************************************************
https://www.interactivebrokers.com/api/doc.html
****************************************************
Open https://localhost:5000 to login
```

Note only one Interactive Brokers session can be active at one time and it's
designed not to be automated. This will be annoying, I guarantee it.

## Usage

```go
import "github.com/kevinburke/ibclientportal"

func main() {
	client := ibclientportal.New("") // defaults to https://localhost:5000
	client.SetInsecureSkipVerify()   // this is bad; you probably want to remove
	contracts, err := client.Contracts.Stocks(ctx, url.Values{
		"symbols": []string{"VOO", "VT"},
	})
	// ...
}
```

## Live quotes: streaming market data (websocket)

The gateway also exposes a websocket at `wss://localhost:5000/v1/api/ws` for
streaming market data ("smd") keyed by contract id (`conid`). Use
`DialStream` to open it; the connection reuses the client's authenticated
session, so the same login and `SetInsecureSkipVerify` caveats apply as for the
REST calls. As with the REST snapshot endpoint, `/iserver/accounts` must have
been called before subscribing.

```go
client := ibclientportal.New("") // defaults to https://localhost:5000
client.SetInsecureSkipVerify()   // self-signed localhost gateway cert

stream, err := client.DialStream(ctx)
if err != nil {
	// ...
}
defer stream.Close()

// 265598 is AAPL. Fields default to last/bid/ask and sizes if omitted.
if err := stream.SubscribeMarketData(265598,
	ibclientportal.FieldLastPrice,
	ibclientportal.FieldBidPrice,
	ibclientportal.FieldAskPrice,
); err != nil {
	// ...
}

for update := range stream.Updates() {
	if last, ok := update.Float(ibclientportal.FieldLastPrice); ok {
		log.Printf("conid %d last=%.2f", update.Conid, last)
	}
}
// The loop ends when the stream closes; check stream.Err() for the cause.
```

Updates are incremental: the gateway sends only the fields that changed, so
maintain your own latest-value state per conid. `MarketDataUpdate.Fields` maps
IB numeric field codes (the `Field*` constants) to raw JSON values;
`String` and `Float` read them regardless of whether IB encoded a given field
as a JSON string or number.

The stream reconnects automatically if the connection drops and replays every
active subscription, so the `for range stream.Updates()` loop keeps running
across reconnects. It also renews subscriptions on a timer: the gateway
terminates a market-data subscription after 10 minutes even on a healthy
connection, so the stream re-sends each active subscription every 9 minutes to
keep the data flowing. Because the context passed to `DialStream` governs those
reconnect attempts, pass one that lives as long as you want the stream (e.g. a
`context.Context` you cancel at shutdown), not a short per-request context. The
loop exits only when you call `stream.Close()` or that context is cancelled.

### Market-data lines

IBKR allows roughly 100 concurrent market-data lines per account. The limit is
account-wide and transport-agnostic: REST snapshots
(`/iserver/marketdata/snapshot`) and websocket `smd` subscriptions draw from the
same pool, as do Trader Workstation watchlists. Streaming a hundred conids
therefore leaves nothing for a snapshot of a hundred-and-first, and IBKR does
not document what happens at that boundary.

`cmd/ibclientportal-mdprobe` answers the question empirically for a given
account. It snapshots one contract while idle to establish a baseline, opens the
websocket and subscribes to `--count` conids, snapshots that same contract again
while saturated, and then watches for streaming conids that fall silent
(displacement). It prints a verdict and, with `--json`, writes the whole record.

```sh
# resolve conids and print the plan without touching market data
ibclientportal-mdprobe --host https://localhost:5000 --insecure --dry-run

# the real thing, keeping the raw websocket frames for inspection
ibclientportal-mdprobe --host https://localhost:5000 --insecure \
    --count 100 --frames --json result.json 2>frames.log
```

Running it consumes `--count` live market-data lines for the duration, so other
consumers of the account may be starved while it runs; prefer a quiet market and
ramp up with a small `--count` first. It releases every line it took —
unsubscribing each conid, closing the stream, and calling
`/iserver/marketdata/unsubscribeall` — including on Ctrl-C.

`--frames` is worth passing: `(*Stream).Updates` delivers only `smd` frames, so
a gateway error or `system` frame is invisible to the structured report but is
logged to stderr. Redirect it and look for frames whose topic is not `smd+`.

## Cash flows: deposits, withdrawals, fees (Flex Web Service)

The Client Portal Gateway does not expose deposit/withdrawal/fee history to
retail accounts, and the OAuth 2.0 endpoints that do are gated behind an
institutional approval process. The path that works for an individual account
is the **Flex Web Service**, a separate IBKR API that downloads Activity Flex
Query reports. Its "Cash Transactions" section reports deposits, withdrawals,
fees, dividends and interest. It needs no Java gateway and no browser login —
just a token and a query ID.

Set up once in IBKR Account Management:

1. *Reports > Settings > Flex Web Service*: enable it and generate a token.
2. *Reports > Flex Queries*: build a Custom Activity Flex Query that includes
   the "Cash Transactions" section, and note its Query ID.

The Flex client lives in its own package, `github.com/kevinburke/ibclientportal/flex`:

```go
import "github.com/kevinburke/ibclientportal/flex"

client := flex.NewClient(token)
report, err := client.Download(ctx, queryID) // requests, then polls until ready
for _, t := range report.CashTransactions() {
	fmt.Println(t.DateTime, t.Type, t.Amount, t.Currency, t.Description)
}
```

The Activity Flex Query model (`flex/flex_sections.go`) includes typed structs
for the sections present in the synthetic schema sample, so a single query can
carry trades, open positions, FX lots, interest accruals, statement of funds,
securities lending, and more. Each section hangs off `flex.Statement` (one per
account):

```go
for _, s := range report.Statements {
	for _, t := range s.Trades.Trade {        // also Lot, Order, SymbolSummary, ...
		fmt.Println(t.Symbol, t.Quantity, t.TradePrice, t.FifoPnlRealized)
	}
	for _, p := range s.OpenPositions.OpenPosition {
		fmt.Println(p.Symbol, p.Position, p.MarkPrice, p.PositionValue)
	}
}
```

Fields bind by attribute name, so selecting any subset of columns is safe;
numeric attributes use `flex.Float` (an empty column decodes to 0), while dates,
identifiers, and codes are kept as strings because their formats are
query-configurable. Sections that were empty in the source sample, such as
transfers or corporate actions in the committed sample, retain their data via a
`flex.RawElement` catch-all rather than dropping it silently.

`flex/flex_sections.go` is generated by `go generate ./flex` (which runs
`flex/gen.py`, requires python3) from `flex/testdata/sample.xml` — a synthetic,
schema-complete sample report containing no real account data. CI fails if the
generated file is out of date.

The sample is produced by `flex/sanitize.py`, which reads a real report only for
its element/attribute *names* and per-attribute type, then rebuilds the tree
with values from a fixed synthetic vocabulary — no real value is ever copied, so
the result is safe by construction rather than by scrubbing. `TestSampleHasNoPrivateData`
enforces this: it fails if `sample.xml` contains any value outside that
vocabulary. To refresh the schema when IBKR adds columns:

```sh
cd flex && python3 sanitize.py --input /path/to/real-report.xml && go generate ./...
```

There is also a command:

```sh
export IBCLIENTPORTAL_FLEX_TOKEN=...
ibclientportal-flex --query 998877
ibclientportal-flex --query 998877 --type "Deposits/Withdrawals" --json
```

## Testing

Some tests exercise live endpoints. To override the default host used in tests,
set `IBCLIENTPORTAL_TEST_HOST`:

```sh
IBCLIENTPORTAL_TEST_HOST=https://localhost:5000 go test -run TestStocks
```

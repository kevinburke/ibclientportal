# History

## Unreleased

- Fix `CancelOrder` failing on every real cancel. The live gateway answers a
  cancel with `"order_id"` as a JSON *number*, though it uses a string for the
  same order when placing it, so decoding into a `string` field failed with
  `json: cannot unmarshal number into Go struct field
  CancelOrderResponse.order_id of type string` — after IB had already cancelled
  the order. The caller saw an error for something that had worked.

  `OrderPlacement.OrderID` and `CancelOrderResponse.OrderID` are now of type
  `OrderID`, a string that decodes from either JSON shape. This is a breaking
  change for callers that assign the field to a `string` variable; add a
  `string(...)` conversion. Placement is included even though it sends a string
  today: the cancel endpoint shows the gateway does not treat the type as part
  of its contract, and a placement that fails to decode leaves an order live
  that the caller believes was rejected.

## 0.10.0 (July 31, 2026)

- Add order placement: `(*OrdersService).PlaceOrders`, `ConfirmOrder`,
  `ModifyOrder`, `CancelOrder` and `WhatIf`. Order submission answers with a
  union of three shapes — a question that must be confirmed before the order
  reaches the market, a placed order, or an error — so `OrderPlacement` exposes
  `IsQuestion`/`IsPlaced`/`Question` rather than leaving callers to guess which
  arrived. The gateway returns some rejections as a bare JSON object with HTTP
  200 instead of the documented array; those are decoded and surfaced as an
  error instead of failing to parse.

- Add `(*MarketDataService).Snapshot`, `Unsubscribe` and `UnsubscribeAll` for
  one-off REST quotes and for releasing market-data lines. `Snapshot` returns
  `Snapshot` values with the same `String`/`Float` field accessors as a
  streaming `MarketDataUpdate`, and reports a field the gateway has not resolved
  yet as absent rather than zero.

## 0.9.0 (July 27, 2026)

- Add streaming market data over the gateway websocket
  (`wss://localhost:5000/v1/api/ws`). `(*Client).DialStream` opens an
  authenticated connection and returns a `*Stream`;
  `(*Stream).SubscribeMarketData(conid, fields...)` subscribes to live quotes
  and `(*Stream).Updates()` delivers `MarketDataUpdate` values. The connection
  reuses the client's session cookies and TLS settings and keeps itself alive
  with periodic keep-alives. `DialStream` checks the session via `/tickle` and
  waits for the gateway's `sts` (session established) frame before returning, so
  a subscription is never sent too early (the gateway silently drops early
  subscriptions). The `Stream` reconnects automatically if the connection
  drops, replaying every active subscription on the new connection; the
  `Updates` channel stays open across reconnects and closes only when `Close` is
  called or the dial context is cancelled. It also renews every active
  subscription every 9 minutes, because the gateway terminates a market-data
  subscription after 10 minutes even on a healthy connection. Field codes are
  exposed as `Field*` constants. Adds a dependency on
  `github.com/gorilla/websocket`.

- Add `cmd/ibclientportal-mdprobe`, a diagnostic that measures what the gateway
  does when the account's ~100 concurrent market-data lines are saturated. It
  snapshots one contract while idle, subscribes to `--count` conids over the
  websocket, snapshots that contract again while saturated, and watches for
  streaming conids that fall silent, then prints a verdict (limit not reached /
  explicit error / silent failure / displacement). `--dry-run` resolves conids
  without touching market data; `--json` writes the full record. The tool always
  releases the lines it took, including on Ctrl-C.

- Fix `New` sharing a single process-wide `*http.Client` and transport across
  all `Client` values (a side effect of `restclient.New`). Each `Client` now
  gets its own `*http.Client`, cookie jar, and cloned transport. Previously two
  Clients shared one cookie jar (leaking session state between them) and
  concurrent `New` calls raced on the shared jar; `SetInsecureSkipVerify` also
  mutated the global `http.DefaultTransport`, disabling TLS verification
  process-wide. This was surfaced by the streaming work, which clones the
  transport's TLS config for the websocket dial.

## 0.8.0 (June 22, 2026)

- Surface HTTP 429 (Too Many Requests) from the gateway as a typed
  `RateLimitError` instead of restclient's opaque "invalid response body:"
  error. The gateway throttles some endpoints (notably `/pa/transactions`, one
  request per 15 minutes per account) and answers a throttled request with an
  empty body, which the default error parser could not distinguish from a real
  failure. `New` now installs an `ErrorParser` that returns `*RateLimitError`
  for 429s and defers to the default parser otherwise. Detect it with
  `errors.As` and back off. The `RetryAfter` field honors a `Retry-After`
  header if one is present, but IB is not known to send one, so it is typically
  0. Because a 429 can mean a multi-minute penalty box, prefer `EnableRateLimits`
  for proactive throttling that avoids most 429s in the first place.

## 0.7.0 (June 9, 2026)

- Add the `flex` package, a client for the Flex Web Service: a separate IBKR API
  that downloads Activity Flex Query reports without the Java gateway or a
  browser login. `flex.Client.Download` runs the two-step SendRequest/GetStatement
  flow and polls while the report is still generating. Parses the Cash
  Transactions section (deposits, withdrawals, fees, dividends, interest) into
  `flex.CashTransaction` records.
- Add the `ibclientportal-flex` command to download and print cash transactions.
- Scaffold Activity Flex Query sections (`flex/flex_sections.go`) from a live
  report sample: typed structs for trades, open positions, FX positions/lots,
  statement of funds, securities lending, interest accruals, transaction taxes,
  and more, plus raw catch-alls for sections that were empty in the source
  sample. Numeric attributes use `flex.Float`, which decodes an empty attribute
  as 0; identifiers and dates stay strings. `flex_sections.go` is generated by
  `go generate ./flex` from a synthetic, schema-complete
  `flex/testdata/sample.xml` (built by `sanitize.py` from a real report, with
  all values replaced from a fixed vocabulary); CI guards against generated-code
  drift, and a test guards the sample against any non-synthetic value.

## 0.6.2 (April 30, 2026)

- Accept the real-time `asOf` transaction marker returned by the IBKR API.

## 0.6.1 (April 30, 2026)

- Add a `make release` target and `Version` constant for release automation.

## 0.6.0 (April 30, 2026)

- Return an error when `includesRealTime` has an invalid value instead of
  silently ignoring malformed IBKR API responses.
- Build CI for all pushed refs.
- Add daily Dependabot updates.
- Bump `golang.org/x/time` from 0.14.0 to 0.15.0.

## 0.5.0 (February 27, 2026)

- Add `Ledger` endpoint to `PortfolioService` (`/portfolio/{accountId}/ledger`)
  with detailed API documentation for all `LedgerEntry` fields.
- Support `IBCLIENTPORTAL_HOST` environment variable as a fallback in `New`.

## 0.4.0 (February 13, 2026)

- Add `PerformanceAnalyticsService` with `ListTransactions` (`/pa/transactions`
  endpoint).
- Add GitHub Actions CI (staticcheck, go vet, go test).
- Add unit tests for stocks, market data history, tickle, and transactions
  response parsing.
- Skip integration tests when running with `-short`.

## 0.3.0 (January 6, 2026)

- Add client-side rate limiting (`RateLimiter`) with per-endpoint rules matching
  IB's documented limits.
- Track the selected account ID on the `Client` and use it for per-account rate
  limit keys.
- Add `EnableRateLimits`, `DisableRateLimits`, and `SetRateLimiter` methods.
- Bundle the IB Client Portal OpenAPI spec in `specs/`.

## 0.2.0 (January 6, 2026)

- Version bump only; no functional changes from 0.1.0.

## 0.1.0 (January 6, 2026)

- Initial tagged release.
- Go module with `go.mod`.
- Client with cookie jar and configurable host.
- Services: `Contracts`, `MarketData`, `Orders`, `Portfolio`,
  `SecurityDefinitions`.
- Endpoints for SSO validate, tickle, search, market data history/snapshot,
  contract details, futures, stocks, portfolio accounts/positions, orders
  (place, preview, list, trades), switch account, and tradable accounts.
- Custom JSON unmarshaler for market data history timestamps.
- Makefile with test and release targets.

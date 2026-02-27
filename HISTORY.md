# History

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

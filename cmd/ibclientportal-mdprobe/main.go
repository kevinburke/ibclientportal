// Command ibclientportal-mdprobe measures empirically what the Interactive
// Brokers Client Portal Gateway does when an account's concurrent market-data
// lines are saturated.
//
// IBKR documents a limit of roughly 100 concurrent market-data lines per
// account. The limit is account-wide and transport-agnostic: REST snapshots
// (/iserver/marketdata/snapshot) and streaming websocket subscriptions ("smd")
// draw from the same pool. What IBKR does not document is the behaviour at the
// boundary: if 100 conids are already streaming and a snapshot is requested for
// a 101st contract, does the gateway return an error, silently return empty
// fields, or displace one of the streaming subscriptions?
//
// The probe answers that by running four phases:
//
//	baseline    snapshot the probe conid with nothing else subscribed, to
//	            establish how long a healthy snapshot takes and what it returns
//	saturate    open the websocket and subscribe to --count conids, then wait
//	            --settle for quotes to arrive, recording which conids ticked
//	probe       snapshot the probe conid again while saturated, using the same
//	            polling policy as the baseline
//	observe     keep reading the stream for --post to see whether any streaming
//	            conid fell silent (evidence of displacement)
//
// It then prints a report comparing the baseline and saturated snapshots, and
// with --json writes the whole result for later analysis.
//
// This perturbs live market data for the account: while it runs, --count lines
// are consumed and other consumers (Trader Workstation, other API clients) may
// be starved. Run it outside of trading hours if that matters, or ramp up with
// a small --count first. The probe always releases what it took: it
// unsubscribes every conid, closes the stream, and calls
// /iserver/marketdata/unsubscribeall, including on Ctrl-C.
//
// Usage:
//
//	# see what it would do, without touching market data
//	ibclientportal-mdprobe --host https://gateway.example --dry-run
//
//	# the real thing, saving raw websocket frames for inspection
//	ibclientportal-mdprobe --host https://gateway.example \
//	    --count 100 --frames --json result.json 2>frames.log
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kevinburke/ibclientportal"
)

// Market data field codes requested in both transports, so the streaming and
// snapshot results are directly comparable. 6509 is the most interesting one
// here: it reports market data availability, e.g. "R" (realtime), "D"
// (delayed), "Z" (frozen) or "N" (not subscribed), which is where a
// line-exhaustion signal would most plausibly show up.
const (
	fieldLastPrice    = ibclientportal.FieldLastPrice
	fieldBidPrice     = ibclientportal.FieldBidPrice
	fieldAskPrice     = ibclientportal.FieldAskPrice
	fieldAvailability = "6509"
	fieldServerID     = "6119"
)

var probeFields = []string{fieldLastPrice, fieldBidPrice, fieldAskPrice, fieldAvailability, fieldServerID}

// defaultSymbols is a pool of liquid US listings used to source conids. Only
// the first --count + 1 that resolve are used; the rest are slack for symbols
// that fail to resolve (delistings, renames, non-US primary listings).
const defaultSymbols = `AAPL MSFT NVDA AMZN GOOGL GOOG META TSLA AVGO LLY
JPM V XOM UNH MA JNJ PG HD COST ORCL MRK ABBV CVX AMD KO PEP ADBE WMT CRM BAC
TMO MCD CSCO ACN ABT LIN NFLX INTC DIS WFC VZ TXN DHR PM INTU AMGN COP CAT NEE
UNP IBM NOW GE SPGI QCOM HON LOW AMAT BA RTX BKNG GS T ELV SBUX PLD DE BLK MDT
SYK LMT ADP GILD MDLZ ADI TJX VRTX MMC AXP C CVS CI SCHW MU BMY ISRG ETN ZTS
REGN SO CB PGR LRCX BSX EOG DUK SLB MO PANW KLAC EQIX ITW CME APD WM SHW MCK
NOC MPC PYPL TGT CSX GD EMR PSX MSI HCA AON FDX MMM F GM NSC ORLY VLO CDNS
SNPS MAR ROP TT AJG TDG PCAR AFL NXPI ECL SRE D TRV ALL KMB EW AIG O PSA AEP
MET PRU STZ DOW KHC EXC HLT ADSK CTAS YUM IDXX ROST GIS KR CNC HUM WELL`

func main() {
	host := flag.String("host", "", "gateway base URL (defaults to $IBCLIENTPORTAL_HOST, then "+ibclientportal.DefaultHost+")")
	insecure := flag.Bool("insecure", false, "skip TLS certificate verification (needed for the default self-signed localhost gateway)")
	count := flag.Int("count", 100, "number of conids to subscribe to over the websocket")
	symbols := flag.String("symbols", "", "comma-separated symbols to resolve to conids (default: a built-in list of liquid US stocks)")
	settle := flag.Duration("settle", 45*time.Second, "how long to let streaming quotes arrive before probing over REST")
	post := flag.Duration("post", 30*time.Second, "how long to keep watching the stream after the REST probe")
	attempts := flag.Int("snapshot-attempts", 10, "how many times to poll /iserver/marketdata/snapshot before giving up")
	interval := flag.Duration("snapshot-interval", 2*time.Second, "delay between snapshot polls")
	delay := flag.Duration("delay", 5*time.Second, "countdown before subscribing, so there is time to Ctrl-C out")
	dryRun := flag.Bool("dry-run", false, "resolve conids and print the plan, but do not subscribe to anything")
	frames := flag.Bool("frames", false, "log every raw websocket frame to stderr; redirect it (2>frames.log) and grep for frames whose topic is not smd+")
	jsonOut := flag.String("json", "", "write the full result to this file as JSON")
	version := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *version {
		fmt.Println("ibclientportal-mdprobe version " + ibclientportal.Version)
		os.Exit(0)
	}
	if *count < 1 {
		slog.Error("--count must be at least 1", "count", *count)
		os.Exit(2)
	}
	if *attempts < 1 {
		slog.Error("--snapshot-attempts must be at least 1", "attempts", *attempts)
		os.Exit(2)
	}
	if *frames {
		// The library logs every received frame under this variable. It is the
		// only way to see non-market-data frames (gateway errors, "system"
		// messages), which (*Stream).Updates deliberately drops.
		os.Setenv("IBCP_WS_DEBUG", "1")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// A trailing slash would produce a doubled slash in every request path.
	client := ibclientportal.New(strings.TrimRight(*host, "/"))
	if *insecure {
		client.SetInsecureSkipVerify()
	}
	client.EnableRateLimits()

	p := &prober{
		client:           client,
		count:            *count,
		symbols:          splitSymbols(*symbols),
		settle:           *settle,
		post:             *post,
		snapshotAttempts: *attempts,
		snapshotInterval: *interval,
		delay:            *delay,
		dryRun:           *dryRun,
	}
	result, err := p.run(ctx)
	if result != nil {
		result.print(os.Stdout)
		if *jsonOut != "" {
			if werr := writeJSON(*jsonOut, result); werr != nil {
				slog.Error("could not write JSON result", "path", *jsonOut, "error", werr)
				if err == nil {
					err = werr
				}
			} else {
				slog.Info("wrote JSON result", "path", *jsonOut)
			}
		}
	}
	if err != nil {
		slog.Error("probe failed", "error", err)
		os.Exit(1)
	}
}

type prober struct {
	client           *ibclientportal.Client
	count            int
	symbols          []string
	settle           time.Duration
	post             time.Duration
	snapshotAttempts int
	snapshotInterval time.Duration
	delay            time.Duration
	dryRun           bool
}

// Result is the full record of one probe run. It is what --json writes.
type Result struct {
	Host          string         `json:"host"`
	StartedAt     time.Time      `json:"started_at"`
	Count         int            `json:"count"`
	DryRun        bool           `json:"dry_run"`
	Fields        []string       `json:"fields"`
	Streamed      []Contract     `json:"streamed"`
	Probe         *Contract      `json:"probe_contract,omitempty"`
	Baseline      *SnapshotRun   `json:"baseline_snapshot,omitempty"`
	Saturated     *SnapshotRun   `json:"saturated_snapshot,omitempty"`
	Subscribed    int            `json:"subscribed"`
	Quoted        int            `json:"quoted_after_settle"`
	NeverQuoted   []int          `json:"never_quoted_conids,omitempty"`
	Availability  map[string]int `json:"availability_histogram,omitempty"`
	WentSilent    []int          `json:"went_silent_after_probe,omitempty"`
	StillTicking  int            `json:"still_ticking_after_probe"`
	MarketMoving  bool           `json:"market_moving"`
	StreamErr     string         `json:"stream_error,omitempty"`
	CleanupErrors []string       `json:"cleanup_errors,omitempty"`
	Notes         []string       `json:"notes,omitempty"`
}

// Contract is a resolved symbol.
type Contract struct {
	Symbol   string `json:"symbol"`
	Conid    int    `json:"conid"`
	Exchange string `json:"exchange"`
}

// SnapshotRun records every poll of /iserver/marketdata/snapshot for one
// contract, so a saturated run can be compared against the baseline.
type SnapshotRun struct {
	Conid        int               `json:"conid"`
	Attempts     []SnapshotAttempt `json:"attempts"`
	Succeeded    bool              `json:"succeeded"`
	AttemptsUsed int               `json:"attempts_used"`
	TimeToQuote  string            `json:"time_to_quote,omitempty"`
	LastPrice    string            `json:"last_price,omitempty"`
	Availability string            `json:"availability,omitempty"`
}

// SnapshotAttempt is one HTTP call to the snapshot endpoint.
type SnapshotAttempt struct {
	Attempt int             `json:"attempt"`
	Elapsed string          `json:"elapsed"`
	Error   string          `json:"error,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

func (p *prober) run(ctx context.Context) (*Result, error) {
	result := &Result{
		Host:      p.client.Base,
		StartedAt: time.Now(),
		Count:     p.count,
		DryRun:    p.dryRun,
		Fields:    probeFields,
	}

	tickle, err := p.client.Tickle(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("tickle: %w", err)
	}
	if !tickle.IServer.AuthStatus.Authenticated {
		return result, fmt.Errorf("brokerage session is not authenticated (%q); log in to the gateway first",
			tickle.IServer.AuthStatus.Message)
	}
	slog.Info("session authenticated", "competing", tickle.IServer.AuthStatus.Competing)

	// /iserver/accounts is a documented prerequisite for market data. It is
	// also the endpoint IBKR throttles most aggressively, so a failure here is
	// a warning: market data usually still works if the session was primed
	// earlier by another client.
	var accounts json.RawMessage
	if err := p.client.ListResource(ctx, "/iserver/accounts", nil, &accounts); err != nil {
		slog.Warn("could not prime the session via /iserver/accounts; market data may be empty", "error", err)
		result.Notes = append(result.Notes, "priming call to /iserver/accounts failed: "+err.Error())
	}

	contracts, err := p.resolveContracts(ctx)
	if err != nil {
		return result, err
	}
	if len(contracts) < p.count+1 {
		return result, fmt.Errorf("resolved only %d conids, need %d (--count plus one unsubscribed probe contract); pass more --symbols",
			len(contracts), p.count+1)
	}
	result.Streamed = contracts[:p.count]
	probe := contracts[p.count]
	result.Probe = &probe
	slog.Info("resolved contracts", "streaming", len(result.Streamed), "probe_symbol", probe.Symbol, "probe_conid", probe.Conid)

	if p.dryRun {
		result.Notes = append(result.Notes, "dry run: nothing was subscribed")
		return result, nil
	}

	// Phase 1: baseline. One snapshot for the probe contract with no streaming
	// subscriptions held by this process, to learn what "healthy" looks like.
	slog.Info("phase 1: baseline snapshot", "conid", probe.Conid, "symbol", probe.Symbol)
	baseline := p.pollSnapshot(ctx, probe.Conid)
	result.Baseline = baseline
	// Release the line the baseline snapshot took, so it does not count against
	// the saturation phase.
	if err := p.unsubscribeREST(ctx, probe.Conid); err != nil {
		slog.Warn("could not release the baseline snapshot subscription", "conid", probe.Conid, "error", err)
	}

	if !p.countdown(ctx) {
		return result, ctx.Err()
	}

	// Phase 2: saturate.
	slog.Info("phase 2: opening the websocket and subscribing", "count", p.count)
	stream, err := p.client.DialStream(ctx)
	if err != nil {
		return result, fmt.Errorf("dialing the streaming websocket: %w", err)
	}
	t := newTracker()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		t.watch(stream.Updates())
	}()
	// Release everything this run took, whatever happens from here on.
	defer func() {
		errs := p.cleanup(stream, result.Streamed, probe.Conid)
		<-watchDone
		if serr := stream.Err(); serr != nil && !errors.Is(serr, context.Canceled) {
			result.StreamErr = serr.Error()
		}
		result.CleanupErrors = append(result.CleanupErrors, errs...)
	}()

	for _, c := range result.Streamed {
		if err := stream.SubscribeMarketData(c.Conid, probeFields...); err != nil {
			return result, fmt.Errorf("subscribing to conid %d (%s): %w", c.Conid, c.Symbol, err)
		}
		result.Subscribed++
	}
	slog.Info("subscribed; waiting for quotes to settle", "subscribed", result.Subscribed, "settle", p.settle)
	if !sleepCtx(ctx, p.settle) {
		return result, ctx.Err()
	}

	afterSettle := t.snapshot()
	for _, c := range result.Streamed {
		st, ok := afterSettle[c.Conid]
		if !ok || !st.HasPrice {
			result.NeverQuoted = append(result.NeverQuoted, c.Conid)
			continue
		}
		result.Quoted++
	}
	result.Availability = availabilityHistogram(afterSettle)
	slog.Info("settle window complete", "quoted", result.Quoted, "never_quoted", len(result.NeverQuoted))

	// Phase 3: the actual question. Snapshot a contract that is not streaming,
	// while the stream holds --count lines.
	slog.Info("phase 3: snapshot under saturation", "conid", probe.Conid, "symbol", probe.Symbol)
	result.Saturated = p.pollSnapshot(ctx, probe.Conid)

	// Phase 4: did anything get displaced?
	slog.Info("phase 4: watching for displacement", "post", p.post)
	beforeProbe := afterSettle
	if !sleepCtx(ctx, p.post) {
		return result, ctx.Err()
	}
	afterProbe := t.snapshot()
	for _, c := range result.Streamed {
		before, wasTicking := beforeProbe[c.Conid]
		after := afterProbe[c.Conid]
		if !wasTicking || !before.HasPrice {
			continue // it never worked; not evidence of displacement
		}
		if after.Updates > before.Updates {
			result.StillTicking++
			continue
		}
		result.WentSilent = append(result.WentSilent, c.Conid)
	}
	sort.Ints(result.WentSilent)
	// With the market closed every contract goes quiet at once, so "went
	// silent" means nothing unless at least one contract kept ticking.
	result.MarketMoving = result.StillTicking > 0
	if !result.MarketMoving {
		result.Notes = append(result.Notes,
			"no contract ticked during the observation window (market closed, or all data is frozen/previous-close): the displacement check is indeterminate")
	}
	return result, nil
}

// resolveContracts turns symbols into conids via /trsrv/stocks, preferring the
// US listing of each stock. It stops once it has count+1 distinct conids.
func (p *prober) resolveContracts(ctx context.Context) ([]Contract, error) {
	want := p.count + 1
	syms := p.symbols
	if len(syms) == 0 {
		syms = strings.Fields(defaultSymbols)
	}

	var out []Contract
	seen := make(map[int]bool)
	const chunk = 50
	for start := 0; start < len(syms) && len(out) < want; start += chunk {
		end := min(start+chunk, len(syms))
		batch := syms[start:end]
		var resp map[string][]stock
		query := url.Values{"symbols": []string{strings.Join(batch, ",")}}
		if err := p.client.ListResource(ctx, "/trsrv/stocks", query, &resp); err != nil {
			return nil, fmt.Errorf("resolving symbols %s: %w", strings.Join(batch, ","), err)
		}
		// Iterate over the request order, not the map, so runs are repeatable.
		for _, sym := range batch {
			if len(out) >= want {
				break
			}
			c, ok := pickUSContract(resp[sym])
			if !ok {
				slog.Warn("could not resolve symbol to a US contract", "symbol", sym)
				continue
			}
			if seen[c.Conid] {
				continue
			}
			seen[c.Conid] = true
			c.Symbol = sym
			out = append(out, c)
		}
	}
	return out, nil
}

// stock is one entry of the /trsrv/stocks response, which maps each requested
// symbol to the listings that match it.
type stock struct {
	Name       string `json:"name"`
	AssetClass string `json:"assetClass"`
	Contracts  []struct {
		Conid    int    `json:"conid"`
		Exchange string `json:"exchange"`
		IsUS     bool   `json:"isUS"`
	} `json:"contracts"`
}

// pickUSContract chooses the US listing of a stock. A symbol commonly resolves
// to several contracts (for example the same company listed in Mexico and on
// ARCA); market data for the US listing is what the account is subscribed to.
func pickUSContract(stocks []stock) (Contract, bool) {
	for _, s := range stocks {
		if s.AssetClass != "" && s.AssetClass != "STK" {
			continue
		}
		for _, c := range s.Contracts {
			if c.IsUS {
				return Contract{Conid: c.Conid, Exchange: c.Exchange}, true
			}
		}
	}
	return Contract{}, false
}

// pollSnapshot calls /iserver/marketdata/snapshot repeatedly for one conid. The
// endpoint is documented to need more than one call: the first returns little
// more than the conid while the backend subscribes. Polling stops as soon as a
// price field appears, or after snapshotAttempts.
func (p *prober) pollSnapshot(ctx context.Context, conid int) *SnapshotRun {
	run := &SnapshotRun{Conid: conid}
	query := url.Values{
		"conids": []string{strconv.Itoa(conid)},
		"fields": []string{strings.Join(probeFields, ",")},
	}
	start := time.Now()
	for i := 1; i <= p.snapshotAttempts; i++ {
		var rows []map[string]json.RawMessage
		err := p.client.ListResource(ctx, "/iserver/marketdata/snapshot", query, &rows)
		attempt := SnapshotAttempt{Attempt: i, Elapsed: time.Since(start).Round(time.Millisecond).String()}
		if err != nil {
			attempt.Error = err.Error()
			slog.Warn("snapshot call failed", "conid", conid, "attempt", i, "error", err)
		} else if body, merr := json.Marshal(rows); merr == nil {
			attempt.Body = body
		}
		run.Attempts = append(run.Attempts, attempt)
		run.AttemptsUsed = i

		if err == nil {
			row := rowForConid(rows, conid)
			if avail := jsonString(row[fieldAvailability]); avail != "" {
				run.Availability = avail
			}
			if price := firstPrice(row); price != "" {
				run.Succeeded = true
				run.LastPrice = price
				run.TimeToQuote = time.Since(start).Round(time.Millisecond).String()
				slog.Info("snapshot returned a price", "conid", conid, "attempt", i,
					"price", price, "availability", run.Availability)
				return run
			}
		}
		if i == p.snapshotAttempts {
			break
		}
		if !sleepCtx(ctx, p.snapshotInterval) {
			break
		}
	}
	slog.Warn("snapshot never returned a price", "conid", conid, "attempts", run.AttemptsUsed,
		"availability", run.Availability)
	return run
}

func (p *prober) unsubscribeREST(ctx context.Context, conid int) error {
	var resp json.RawMessage
	return p.client.UpdateResource(ctx, "/iserver/marketdata/unsubscribe",
		map[string]int{"conid": conid}, &resp)
}

// cleanup releases every market-data line this run took. It uses its own
// context because the caller's is typically already cancelled (Ctrl-C).
func (p *prober) cleanup(stream *ibclientportal.Stream, streamed []Contract, probeConid int) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var errs []string
	slog.Info("releasing market data lines", "conids", len(streamed)+1)
	for _, c := range streamed {
		if err := stream.UnsubscribeMarketData(c.Conid); err != nil {
			errs = append(errs, fmt.Sprintf("websocket unsubscribe conid %d: %v", c.Conid, err))
		}
	}
	if err := stream.Close(); err != nil {
		errs = append(errs, "closing the stream: "+err.Error())
	}
	if err := p.unsubscribeREST(ctx, probeConid); err != nil {
		errs = append(errs, fmt.Sprintf("REST unsubscribe conid %d: %v", probeConid, err))
	}
	// The belt-and-braces step: tell the gateway to drop every backend stream
	// it holds for this session, in case a umd was lost with the connection.
	var resp json.RawMessage
	if err := p.client.ListResource(ctx, "/iserver/marketdata/unsubscribeall", nil, &resp); err != nil {
		errs = append(errs, "unsubscribeall: "+err.Error())
	}
	for _, e := range errs {
		slog.Warn("cleanup problem", "detail", e)
	}
	return errs
}

// countdown gives the operator a window to Ctrl-C before live market data is
// disturbed. It returns false if the context was cancelled.
func (p *prober) countdown(ctx context.Context) bool {
	if p.delay <= 0 {
		return ctx.Err() == nil
	}
	slog.Warn("about to consume market data lines for this account; press Ctrl-C to abort",
		"lines", p.count, "starting_in", p.delay)
	return sleepCtx(ctx, p.delay)
}

// conidState is the running per-conid view of the stream.
type conidState struct {
	Updates      int       `json:"updates"`
	FirstAt      time.Time `json:"first_at"`
	LastAt       time.Time `json:"last_at"`
	HasPrice     bool      `json:"has_price"`
	LastPrice    string    `json:"last_price,omitempty"`
	Availability string    `json:"availability,omitempty"`
}

type tracker struct {
	mu    sync.Mutex
	state map[int]*conidState
}

func newTracker() *tracker {
	return &tracker{state: make(map[int]*conidState)}
}

func (t *tracker) watch(updates <-chan ibclientportal.MarketDataUpdate) {
	for u := range updates {
		t.mu.Lock()
		st := t.state[u.Conid]
		if st == nil {
			st = &conidState{FirstAt: time.Now()}
			t.state[u.Conid] = st
		}
		st.Updates++
		st.LastAt = time.Now()
		for _, f := range []string{fieldLastPrice, fieldBidPrice, fieldAskPrice} {
			if v, ok := u.String(f); ok && v != "" {
				st.HasPrice = true
				if f == fieldLastPrice {
					st.LastPrice = v
				}
			}
		}
		if v, ok := u.String(fieldAvailability); ok && v != "" {
			st.Availability = v
		}
		t.mu.Unlock()
	}
}

func (t *tracker) snapshot() map[int]conidState {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[int]conidState, len(t.state))
	for conid, st := range t.state {
		out[conid] = *st
	}
	return out
}

func availabilityHistogram(states map[int]conidState) map[string]int {
	hist := make(map[string]int)
	for _, st := range states {
		key := st.Availability
		if key == "" {
			key = "(none reported)"
		}
		hist[key]++
	}
	return hist
}

func rowForConid(rows []map[string]json.RawMessage, conid int) map[string]json.RawMessage {
	for _, row := range rows {
		var got int
		if err := json.Unmarshal(row["conid"], &got); err == nil && got == conid {
			return row
		}
	}
	if len(rows) == 1 {
		return rows[0]
	}
	return nil
}

func firstPrice(row map[string]json.RawMessage) string {
	for _, f := range []string{fieldLastPrice, fieldBidPrice, fieldAskPrice} {
		if s := jsonString(row[f]); s != "" {
			return s
		}
	}
	return ""
}

// jsonString reads a value that IB may encode as either a JSON string or a
// JSON number.
func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func (r *Result) print(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== market data line probe =========================================")
	fmt.Fprintf(w, "host:            %s\n", r.Host)
	fmt.Fprintf(w, "started:         %s\n", r.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "streaming conids: %d\n", len(r.Streamed))
	if r.Probe != nil {
		fmt.Fprintf(w, "probe contract:  %s (conid %d, %s) — never subscribed over the websocket\n",
			r.Probe.Symbol, r.Probe.Conid, r.Probe.Exchange)
	}
	if r.DryRun {
		fmt.Fprintln(w, "\ndry run: no subscriptions were made.")
		for _, c := range r.Streamed {
			fmt.Fprintf(w, "  %-6s %d (%s)\n", c.Symbol, c.Conid, c.Exchange)
		}
		return
	}

	fmt.Fprintln(w, "\n--- streaming ------------------------------------------------------")
	fmt.Fprintf(w, "subscribed:      %d\n", r.Subscribed)
	fmt.Fprintf(w, "quoted a price:  %d\n", r.Quoted)
	fmt.Fprintf(w, "never quoted:    %d", len(r.NeverQuoted))
	if n := len(r.NeverQuoted); n > 0 {
		fmt.Fprintf(w, "  %v", r.NeverQuoted[:min(n, 12)])
		if n > 12 {
			fmt.Fprintf(w, " ... (%d more)", n-12)
		}
	}
	fmt.Fprintln(w)
	if len(r.Availability) > 0 {
		fmt.Fprintf(w, "availability:    %s\n", formatHistogram(r.Availability))
	}

	fmt.Fprintln(w, "\n--- snapshot of the extra contract ---------------------------------")
	printSnapshot(w, "baseline (idle)   ", r.Baseline)
	printSnapshot(w, "saturated         ", r.Saturated)
	fmt.Fprintf(w, "verdict:          %s\n", verdict(r))

	fmt.Fprintln(w, "\n--- displacement ---------------------------------------------------")
	fmt.Fprintf(w, "still ticking after the snapshot: %d\n", r.StillTicking)
	fmt.Fprintf(w, "went silent after the snapshot:   %d", len(r.WentSilent))
	if n := len(r.WentSilent); n > 0 {
		fmt.Fprintf(w, "  %v", r.WentSilent[:min(n, 12)])
		if n > 12 {
			fmt.Fprintf(w, " ... (%d more)", n-12)
		}
	}
	fmt.Fprintln(w)
	if r.MarketMoving {
		fmt.Fprintln(w, "note: a thinly traded contract can go quiet on its own; treat a small")
		fmt.Fprintln(w, "      silent count as noise, and a large one as displacement.")
	} else {
		fmt.Fprintln(w, "note: nothing ticked at all, so every contract counts as silent and the")
		fmt.Fprintln(w, "      displacement check says nothing. Re-run during market hours.")
	}

	if r.StreamErr != "" {
		fmt.Fprintf(w, "\nstream ended with: %s\n", r.StreamErr)
	}
	for _, e := range r.CleanupErrors {
		fmt.Fprintf(w, "cleanup: %s\n", e)
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	fmt.Fprintln(w)
}

func printSnapshot(w io.Writer, label string, run *SnapshotRun) {
	if run == nil {
		fmt.Fprintf(w, "%s not run\n", label)
		return
	}
	switch {
	case run.Succeeded:
		fmt.Fprintf(w, "%s price %s after %d call(s) in %s (availability %q)\n",
			label, run.LastPrice, run.AttemptsUsed, run.TimeToQuote, run.Availability)
	default:
		fmt.Fprintf(w, "%s NO PRICE after %d call(s) (availability %q)\n",
			label, run.AttemptsUsed, run.Availability)
	}
	for _, a := range run.Attempts {
		if a.Error != "" {
			fmt.Fprintf(w, "%s   attempt %d at %s: error: %s\n", label, a.Attempt, a.Elapsed, a.Error)
		}
	}
}

// verdict states, in one line, what the two snapshots imply. It deliberately
// describes only what was observed.
func verdict(r *Result) string {
	if r.Baseline == nil || r.Saturated == nil {
		return "inconclusive: one of the snapshots did not run"
	}
	switch {
	case !r.Baseline.Succeeded:
		return "inconclusive: the baseline snapshot did not return a price either, so the saturated result says nothing about the line limit"
	case r.Saturated.Succeeded && !r.MarketMoving:
		return "the extra snapshot worked; displacement is indeterminate because nothing ticked during the observation window"
	case r.Saturated.Succeeded && len(r.WentSilent) == 0:
		return "the extra snapshot worked and nothing was displaced: the limit was not reached at this --count"
	case r.Saturated.Succeeded:
		return "the extra snapshot worked, but streaming conids fell silent: the gateway appears to displace an existing subscription"
	case hasSnapshotError(r.Saturated):
		return "the extra snapshot failed with an error (see the attempts above): the gateway rejects requests past the limit"
	default:
		return "the extra snapshot returned no price and no error: the gateway appears to fail silently past the limit"
	}
}

func hasSnapshotError(run *SnapshotRun) bool {
	for _, a := range run.Attempts {
		if a.Error != "" {
			return true
		}
	}
	return false
}

func formatHistogram(hist map[string]int) string {
	keys := make([]string, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if hist[keys[i]] != hist[keys[j]] {
			return hist[keys[i]] > hist[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, hist[k]))
	}
	return strings.Join(parts, " ")
}

func splitSymbols(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, strings.ToUpper(part))
		}
	}
	return out
}

// sleepCtx waits for d, returning false if the context was cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func writeJSON(path string, result *Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

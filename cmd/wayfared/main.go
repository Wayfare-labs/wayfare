// Command wayfared serves the Wayfare corridor monitor and runs its scheduler.
//
//	wayfared                          # serve on :8080 and measure every 6h
//	wayfared -serve=false             # scheduler only, no HTTP
//	wayfared -schedule=0              # server only, no scheduled measurement
//	wayfared -once                    # one sweep, record, exit (CI schedules this)
//	wayfared -verify-store            # walk the hash chains and exit
//	wayfared -verify-registry         # re-fetch every issuer's stellar.toml and exit
//	wayfared -export USDC-NGNC        # export one verified chain as CSV and exit
//	wayfared -rotate-store            # trim chains over the window ceiling and exit
//	wayfared -check-fresh             # exit non-zero if any record is stale
//	wayfared -version                 # print the build provenance and exit
//
// The two halves are independent on purpose. A monitor that only measures
// while somebody has a page open would leave holes in its history exactly when
// nobody was watching, so the scheduler does not need the server; and the
// server serves live measurements with no scheduler at all.
//
// The service is read-only. It holds no keys, signs nothing, and moves no
// funds; every request it serves is a measurement of public data.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Wayfare-labs/wayfare"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/buildinfo"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/failover"
	"github.com/Wayfare-labs/wayfare/metrics"
	"github.com/Wayfare-labs/wayfare/monitor"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/server"
	"github.com/Wayfare-labs/wayfare/snapshot"
	"github.com/Wayfare-labs/wayfare/transport"
)

// The wait each upstream package gives a client it builds for itself: 20s in
// dex.Client.httpClient and 10s in each of refrate's three providers. Wiring an
// instrumented client replaces that default client, so the number has to be
// copied rather than chosen — metrics.NewClient states the rule, and each copy
// names where it came from. checks needs neither: it builds a client to pass in,
// so it takes the guarded client's own timeout back (see
// instrumentedChecksRunner).
const (
	horizonTimeout   = 20 * time.Second
	referenceTimeout = 10 * time.Second
)

func main() {
	var (
		addr    = flag.String("addr", defaultAddr(), "listen address (default follows $PORT when set)")
		horizon = flag.String("horizon", "", "Horizon base URL (default: mainnet)")
		// Issue #701, the Option B stopgap from docs/horizon-rpc-migration.md:
		// a second, independently operated Horizon for the pathfinding traffic
		// only. Empty means one Horizon and today's behaviour, which is why the
		// default is empty rather than a host this project does not run.
		horizonSecondary = flag.String("horizon-secondary", "",
			"fallback Horizon base URL, used only when the first is down; empty disables failover")
		timeout = flag.Duration("timeout", 90*time.Second,
			"ceiling on a live measurement; the budget per request scales with rung count up to this")
		shutdownGrace = flag.Duration("shutdown-grace", 30*time.Second,
			"how long SIGTERM waits for in-flight requests before closing")
		dataDir        = flag.String("data", envOr("WAYFARE_DATA_DIR", ""), "directory for the run store; empty disables history")
		schedule       = flag.Duration("schedule", monitor.DefaultInterval, "measurement interval; 0 disables the scheduler")
		scheduleJitter = flag.Duration("schedule-jitter", monitor.DefaultJitter,
			"maximum random delay added to each measurement interval; 0 disables jitter")
		serve     = flag.Bool("serve", true, "serve HTTP")
		verify    = flag.Bool("verify-store", false, "verify every corridor chain and exit")
		verifyReg = flag.Bool("verify-registry", false,
			"re-fetch every registered issuer's stellar.toml and report whether each entry still matches; exit 1 if any changed, 2 if any could not be fetched")
		rotate    = flag.Bool("rotate-store", false, "trim every chain over the window ceiling and exit")
		rotateRec = flag.Int("rotate-records", runstore.MaxWindow,
			"per-corridor record ceiling for -rotate-store")
		once     = flag.Bool("once", false, "measure every corridor once, record, and exit")
		exportTo = flag.String("export", "",
			"export one corridor's verified history to stdout and exit, e.g. -export USDC-NGNC; requires -format")
		exportFmt = flag.String("format", exportFormatCSV,
			"export format for -export; csv is the only implemented format")
		exportStatic = flag.String("export-static", "",
			"write a static corridors.json snapshot of all corridors to the given file path and exit")
		checkFresh = flag.Bool("check-fresh", false,
			"report corridors whose newest record is older than -max-age (or missing) and exit; 1 stale, 2 check unavailable")
		maxAge = flag.Duration("max-age", 2*monitor.DefaultInterval,
			"staleness ceiling for -check-fresh; default is two missed sweeps")
		refMaxAge = flag.Duration("ref-max-age", refrate.DefaultMaxAge,
			"maximum allowed age of a reference rate (default: 72h)")
		histFirst = flag.Bool("history-first", false,
			"serve the stored run instead of measuring, unless a request asks for ?live=1")
		// Per-client request bounds (issue #320). A live measurement is a
		// dozen Horizon round trips and the free deployment has nothing in
		// front of it, so a default is set rather than left open. Zero
		// disables limiting for an operator who has their own proxy in front.
		rateLimit = flag.Float64("rate-limit", defaultRateLimit,
			"per-client requests per second; 0 disables rate limiting")
		rateBurst = flag.Int("rate-burst", defaultRateBurst,
			"per-client instantaneous request allowance")
		// Outbound Horizon budget (issue #700). Horizon's public limit is per
		// IP and counts the sweep and live requests together, so one bucket is
		// shared by both; 12/s sits under it with room for one retry burst.
		horizonRate = flag.Float64("horizon-rate-limit", defaultHorizonRate,
			"outbound Horizon requests per second shared by sweeps and live requests; 0 disables the budget")
		horizonBurst = flag.Float64("horizon-rate-burst", defaultHorizonBurst,
			"outbound Horizon instantaneous request allowance")
		logLevel = flag.String("log-level", envOr("WAYFARE_LOG_LEVEL", "info"), "debug, info, warn or error")
		version  = flag.Bool("version", false,
			"print the build provenance (code commit, embedded data commit, Go version) and exit")
	)
	flag.Parse()

	// Version answers before the logger, the run store and any upstream call,
	// so it works with no network and no data directory — the two situations
	// an operator most needs to know which binary they are holding.
	if *version {
		printVersion(os.Stdout, progName)
		return
	}
	if *refMaxAge <= 0 {
		fmt.Fprintln(os.Stderr, "-ref-max-age must be positive")
		os.Exit(2)
	}

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	// -verify-registry is a read-only answer about the compiled-in registry,
	// so it runs before the store is opened: a broken or empty data directory
	// has nothing to do with whether an issuer's stellar.toml still declares
	// its asset, and must not be able to change the answer.
	if *verifyReg {
		os.Exit(verifyRegistryCmd(context.Background(), asset.Registry(), nil, os.Stdout, logger))
	}

	store, verification, err := openStore(*dataDir, logger)
	if err != nil {
		// Open verifies every chain, so a broken one surfaces here. Under
		// -verify-store that is the answer being asked for rather than a
		// startup failure, so it is reported as a result: an operator
		// running the post-deploy check wants the verdict on stdout, not
		// an error log they have to interpret.
		if *verify {
			fmt.Printf("FAIL %v\n", err)
			os.Exit(1)
		}
		logger.Error("opening run store", "error", err)
		os.Exit(1)
	}

	if *verify {
		os.Exit(verifyStore(store, *dataDir, logger))
	}

	if *rotate {
		os.Exit(rotateStore(store, *rotateRec, logger))
	}

	// Export is a read-only answer like -verify-store: it writes the document
	// to stdout and exits without serving or measuring. It refuses a chain
	// that does not verify, so the artifact a researcher keeps is always one
	// this project is willing to stand behind.
	if *exportTo != "" {
		os.Exit(exportChain(store, *exportTo, *exportFmt, os.Stdout, logger))
	}

	// Export a static corridors.json snapshot of all corridors.
	// This is the wire shape for the corridors.json file published on every sweep.
	if *exportStatic != "" {
		os.Exit(exportStaticSnapshot(store, *exportStatic, logger))
	}

	// The alerting hook for scheduled measurement (issue #310): a CI or cron
	// wrapper runs -once and then -check-fresh, so a sweep that silently
	// stopped recording — or recorded but dropped a corridor — exits
	// non-zero and makes the job fail loudly. Needs no network: it only
	// reads the store, so it also catches the case where the schedule never
	// ran at all, which no in-process failure could report.
	if *checkFresh {
		os.Exit(checkFreshness(store, monitor.DefaultCorridors(), *maxAge, time.Now().UTC(), logger))
	}

	if *checkFresh {
		os.Exit(checkFreshness(store, monitor.DefaultCorridors(), *maxAge, time.Now().UTC(), logger))
	}

	if !*once && !*serve && *schedule == 0 {
		logger.Error("nothing to do: -serve=false with -schedule=0")
		os.Exit(2)
	}

	dex.SetLogger(logger)
	refrate.SetLogger(logger)
	server.SetLogger(logger)

	// One collector for the process (issue #705). It is passed to the HTTP
	// server, the scheduler and the limiter, and it is what each upstream
	// client below records through. Nothing else is constructed with it: the
	// counters describe this process, which is what the exposition says, and
	// a second collector would double-count or split one request across two
	// answers.
	collector := metrics.New()

	// One outbound budget for the process (issue #700), shared by the live
	// client, the scheduled sweep and every request the server serves, because
	// the quota is per upstream host rather than per caller. It is nil unless
	// both the rate and the burst are configured, and a nil budget lets
	// everything through.
	var outbound *transport.Budget
	if *horizonRate > 0 && *horizonBurst > 0 {
		outbound = transport.NewBudget(*horizonRate, *horizonBurst)
	}

	// Horizon failover (issue #701). Both addresses are parsed here, at startup,
	// rather than on the first outage: a typo in the fallback is invisible until
	// the moment it is the only way to answer, which is the worst possible time
	// to discover it.
	//
	// The order matters twice over. Outside the metrics transport, so each
	// attempt is the exchange the counters describe and a failover reads as one
	// server_error plus one success rather than as a healthy upstream. And
	// inside the client's timeout, so the two attempts share the one budget a
	// rung was given; a primary that merely runs out of time is not retried
	// against somebody else's service.
	horizonFailover, err := failover.New(horizonBase(*horizon), *horizonSecondary,
		metrics.NewTransport(metrics.UpstreamHorizon, collector, nil))
	if err != nil {
		logger.Error("configuring Horizon failover", "error", err)
		os.Exit(2)
	}

	dexClient := &dex.Client{
		HorizonURL: *horizon,
		// horizonTimeout is dex.Client's own default, copied rather than
		// chosen: replacing the client it builds for itself must not
		// change how long a rung is allowed to take.
		HTTPClient: &http.Client{Transport: horizonFailover, Timeout: horizonTimeout},
		Logger:     logger,
		Budget:     outbound,
	}
	engine := &route.Engine{
		DEX: dexClient,
		// Records the ledger range each ladder's rungs were priced across.
		Ledgers: dexClient,
		// Two independent providers, each cached, cross-checked for
		// divergence. Caching matters here specifically: a ladder is a
		// dozen quotes that would otherwise each fetch the same mid.
		RefRate: checkedReferenceCross(
			&refrate.Cached{Inner: &refrate.ExchangeRateAPI{
				Client: metrics.NewClient(metrics.UpstreamExchangeRateAPI, collector, referenceTimeout),
				Logger: logger,
			}},
			&refrate.Cached{Inner: &refrate.CurrencyAPI{
				Client: metrics.NewClient(metrics.UpstreamCurrencyAPI, collector, referenceTimeout),
				Logger: logger,
			}},
			// The third provider is consulted only when the first two
			// disagree, and only to attribute which of them is the outlier.
			// It never contributes a scored mid (see refrate.Cross). Cached
			// because a sweep prices many rungs and a diagnostic is a
			// daily-published figure, not a per-rung one.
			&refrate.Cached{Inner: &refrate.FloatRates{
				Client: metrics.NewClient(metrics.UpstreamFloatRates, collector, referenceTimeout),
				Logger: logger,
			}},
			*refMaxAge,
		),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One sweep and exit. This is how a scheduled CI job drives the monitor:
	// the runner is the clock, so the process does not need to be long-lived,
	// and the chain it appends to is the same one a hosted instance would
	// write. Nothing about the measurement differs.
	//
	// Built once for either use: the -once path and the interval path configure
	// the same snapshots and the same failover, and a second construction would
	// be a second place for the two to disagree.
	sched, err := newScheduler(engine, store, *dataDir, *horizon, *horizonSecondary, *refMaxAge, outbound, logger, collector)
	if err != nil {
		logger.Error("configuring the scheduler", "error", err)
		os.Exit(2)
	}

	if *once {
		// Sweeps carry their priority on the context: it is what lets a run
		// finish when the budget is otherwise spent (issue #700).
		if err := sched.RunOnce(transport.WithPriority(ctx, transport.Sweep)); err != nil {
			logger.Error("sweep failed", "error", err)
			os.Exit(1)
		}
		return
	}

	var wg sync.WaitGroup

	if *schedule > 0 {
		sched.Interval = *schedule
		sched.Jitter = *scheduleJitter
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sched.Run(transport.WithPriority(ctx, transport.Sweep)); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("scheduler stopped", "error", err)
			}
		}()
	} else {
		logger.Warn("scheduler disabled; history will only grow if another instance is writing")
	}

	if *serve {
		limiter := server.NewRateLimiter(*rateLimit, *rateBurst)
		srv := &server.Server{
			Engine:       engine,
			Budget:       outbound,
			Store:        store,
			Timeout:      *timeout,
			HistoryFirst: *histFirst,
			Checks:       instrumentedChecksRunner(*horizon, collector),
			// The limiter is always mounted; a non-positive -rate-limit or
			// -rate-burst makes it approve everything rather than making it
			// absent. Server.limit gives it this same collector, so the
			// refusals count in the document this process serves.
			Limiter: limiter,
			Metrics: collector,
			// The startup verdict on the history this process serves. /readyz
			// answers with it; /healthz never looks at it.
			Verification: &verification,
		}
		httpSrv := &http.Server{
			Addr:              *addr,
			Handler:           srv.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			// A measurement is a dozen sequential round trips, so the
			// write timeout must exceed the measurement timeout or the
			// response is cut off mid-flight.
			WriteTimeout: *timeout + 30*time.Second,
			IdleTimeout:  60 * time.Second,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ctx.Done()
			shutdownServer(httpSrv, *shutdownGrace, logger)
		}()

		logger.Info("listening", "addr", *addr, "schedule", schedule.String(),
			"history", *dataDir != "")
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "error", err)
			stop()
		}
	} else {
		logger.Info("running scheduler only", "schedule", schedule.String())
	}

	wg.Wait()
}

// defaultAddr is the listen address when -addr is not given: the platform's
// $PORT if set (Render, Fly and Heroku all assign one), else :8080. Being the
// flag default rather than a post-parse override means an explicit -addr,
// even -addr=:8080, always wins.
func defaultAddr() string {
	if p := os.Getenv("PORT"); p != "" {
		return "0.0.0.0:" + p
	}
	return ":8080"
}

// shutdownServer stops accepting connections and waits up to grace for
// in-flight requests, such as a live ladder, to finish.
func shutdownServer(srv *http.Server, grace time.Duration, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown did not complete within the grace period", "grace", grace.String(), "error", err)
	}
}

// checkedReferenceCross applies the absolute freshness policy outside each
// cache. Cached replies are therefore rechecked on every use, and one stale
// source degrades through Cross rather than becoming an apparently fresh hit.
func checkedReferenceCross(primary, secondary, diagnostic refrate.Provider, maxAge time.Duration) *refrate.Cross {
	checked := func(provider refrate.Provider) refrate.Provider {
		if provider == nil {
			return nil
		}
		return &refrate.Checked{Inner: provider, MaxAge: maxAge}
	}
	return &refrate.Cross{
		Primary:    checked(primary),
		Secondary:  checked(secondary),
		Diagnostic: checked(diagnostic),
	}
}

// newScheduler wires the scheduled sweep, including the snapshot archive when a
// data directory was given.
//
// The error is only ever about -horizon-secondary: everything else is a
// constructor. It is still fatal rather than logged-and-ignored, because a
// fallback host that was rejected at startup is one that will not be there
// during the outage it was configured for.
func newScheduler(engine *route.Engine, store runstore.Store, dataDir, horizon, horizonSecondary string,
	refMaxAge time.Duration, budget *transport.Budget, logger *slog.Logger, collector *metrics.Collector) (*monitor.Scheduler, error) {
	sched := &monitor.Scheduler{
		Engine: engine, Store: store, Logger: logger, Metrics: collector,
		Checks: instrumentedChecksRunner(horizon, collector),
	}
	if dataDir == "" {
		return sched, nil
	}

	sched.SnapshotDir = filepath.Join(dataDir, "snapshots")
	horizonURL := horizonBase(horizon)
	sched.SnapshotSources = snapshot.Sources{
		Horizon: snapshot.SourceRef{BaseURL: horizonURL},
		Reference: snapshot.SourceRef{
			Provider: "exchangerate-api", BaseURL: refrate.DefaultExchangeRateAPI,
		},
	}
	sched.SnapshotNotes = []string{
		"secondary reference provider: currency-api at " + refrate.DefaultCurrencyAPI,
		"diagnostic reference provider: floatrates at " + refrate.DefaultFloatRates,
	}
	guardedTransport := checks.GuardedClient(30 * time.Second).Transport
	// The same failover (issue #701) under a recording run, and deliberately in
	// a different place in the stack than on the live path: beneath the recorder
	// instead of above it. The recorder stands under every upstream of the run,
	// so this transport also sees reference-rate and check traffic and moves only
	// what is aimed at the Horizon it fronts. And because the archive is what a
	// later reader replays, the decision has to happen below it: a recorder above
	// the failover would keep the primary's discarded 500 as the fixture for a
	// query the second host actually answered.
	snapshotFailover, err := failover.New(horizonURL, horizonSecondary, guardedTransport)
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}
	sched.SnapshotEngineFactory = func(recorder *snapshot.Recorder) *route.Engine {
		recorder.Transport = snapshotFailover
		// Each upstream still gets its own labelled transport, and the
		// recorder stays underneath it: the recorder is what reads and keeps
		// the response body, so putting it on top would count exchanges whose
		// bytes were never captured, and the snapshot would be empty.
		client := func(upstream string) *http.Client {
			return &http.Client{
				Transport: metrics.NewTransport(upstream, collector, recorder),
				Timeout:   30 * time.Second,
			}
		}
		snapshotDEX := &dex.Client{
			HorizonURL: horizon,
			HTTPClient: client(metrics.UpstreamHorizon),
			Logger:     logger,
			Budget:     budget,
		}
		return &route.Engine{
			DEX: snapshotDEX,
			// The root lookups go through the same recorder, so the ledger a
			// stored range names is itself in the snapshot evidence.
			Ledgers: snapshotDEX,
			RefRate: checkedReferenceCross(
				&refrate.Cached{Inner: &refrate.ExchangeRateAPI{Client: client(metrics.UpstreamExchangeRateAPI), Logger: logger}},
				&refrate.Cached{Inner: &refrate.CurrencyAPI{Client: client(metrics.UpstreamCurrencyAPI), Logger: logger}},
				&refrate.Cached{Inner: &refrate.FloatRates{Client: client(metrics.UpstreamFloatRates), Logger: logger}},
				refMaxAge,
			),
		}
	}
	sched.SnapshotChecksFactory = func(recorder *snapshot.Recorder) *checks.Runner {
		return &checks.Runner{
			HorizonURL: horizon,
			HTTPClient: &http.Client{
				Transport: metrics.NewTransport(metrics.UpstreamChecks, collector, recorder),
				Timeout:   30 * time.Second,
			},
		}
	}
	return sched, nil
}

// horizonBase resolves the -horizon flag the way dex.Client resolves it, so the
// failover transport compares requests against the origin the client actually
// dials rather than against an empty flag.
func horizonBase(raw string) string {
	if raw == "" {
		return dex.DefaultHorizonURL
	}
	return raw
}

// instrumentedChecksRunner builds the counterparty-check runner for a live
// deployment, with its traffic counted.
//
// The client here cannot be metrics.NewClient. Every URL a check reaches — a
// stellar.toml, a declared auth endpoint — is published by the party being
// audited, so checks use an SSRF-guarded client that refuses private and
// loopback addresses and re-validates each redirect hop. Replacing that with a
// plain instrumented client would have traded the guard for a counter. Instead
// the guard stays where it is, and the instrumentation wraps its transport and
// carries back its timeout and redirect policy unchanged.
func instrumentedChecksRunner(horizon string, collector *metrics.Collector) *checks.Runner {
	guarded := checks.GuardedClient(0) // zero takes the package's own default
	return &checks.Runner{
		HorizonURL: horizon,
		HTTPClient: &http.Client{
			Transport:     metrics.NewTransport(metrics.UpstreamChecks, collector, guarded.Transport),
			Timeout:       guarded.Timeout,
			CheckRedirect: guarded.CheckRedirect,
		},
	}
}

// openStore opens the run store, or returns a no-op store when history is off,
// along with what it managed to prove about the history it is serving.
//
// A store that fails to open is fatal rather than silently degraded: on a
// deployment with a volume attached, a failure here means the volume did not
// mount, and running anyway would record nothing while appearing healthy.
//
// The second return value is the reason the fallbacks below are allowed to
// keep serving at all. Opening a store verifies every chain it loads, so a
// store that came back without a check is a store this process cannot vouch
// for — and it says so on /readyz instead of letting a platform route traffic
// to it on the strength of /healthz (issue #704).
func openStore(dir string, logger *slog.Logger) (runstore.Store, server.Verification, error) {
	if dir == "" {
		// No writable directory, but the binary carries the published
		// history and verified it at build time. Serving it is better than
		// serving nothing: the image is built to be self-contained, and a
		// deployment on an ephemeral filesystem is the normal case for it.
		//
		// This store is read-only, so a measurement taken here is reported
		// and then discarded rather than silently appearing to be recorded.
		embedded, err := runstore.OpenFS(wayfare.History, "data")
		if err != nil {
			logger.Warn("embedded history unavailable; measurements will not be recorded",
				"error", err)
			return runstore.Nop{}, server.Verification{
				Note: fmt.Sprintf("embedded history did not open or verify: %v", err),
			}, nil
		}
		corridors, _ := embedded.Corridors(context.Background())
		if len(corridors) == 0 {
			logger.Warn("embedded history is empty; measurements will not be recorded")
			return runstore.Nop{}, server.Verification{
				Note: "embedded history holds no corridors",
			}, nil
		}
		logger.Info("serving embedded history (read-only)",
			"corridors", len(corridors))
		return embedded, server.Verification{
			ChainVerified: true,
			Note:          fmt.Sprintf("embedded history verified at open across %d corridors", len(corridors)),
		}, nil
	}
	store, err := runstore.Open(dir)
	if err != nil {
		return nil, server.Verification{}, err
	}
	corridors, _ := store.Corridors(context.Background())
	logger.Info("run store open", "dir", dir, "corridors", len(corridors))
	return store, server.Verification{
		ChainVerified: true,
		Note:          fmt.Sprintf("run store verified at open across %d corridors", len(corridors)),
	}, nil
}

// verifyStore walks every chain and reports. Intended for use after a deploy
// and after any restore from backup.
func verifyStore(store runstore.Store, dataDir string, logger *slog.Logger) int {
	if store == nil {
		logger.Error("-verify-store requires a store; none is configured")
		return 2
	}
	fs, _ := store.(*runstore.FileStore)

	ctx := context.Background()
	corridors, err := store.Corridors(ctx)
	if err != nil {
		logger.Error("listing corridors", "error", err)
		return 1
	}
	if len(corridors) == 0 {
		fmt.Println("no corridor history to verify")
		return 0
	}

	failed := 0
	for _, c := range corridors {
		if err := store.Verify(ctx, c); err != nil {
			fmt.Printf("FAIL %s: %v\n", c, err)
			failed++
			continue
		}
		if fs != nil {
			if err := fs.VerifySnapshots(ctx, c, filepath.Join(dataDir, "snapshots")); err != nil {
				fmt.Printf("FAIL %s: %v\n", c, err)
				failed++
				continue
			}
		}
		latest, err := store.Latest(ctx, c)
		if err != nil || latest == nil {
			fmt.Printf("FAIL %s: latest read error: %v\n", c, err)
			failed++
			continue
		}
		// Time ordering is reported here rather than at Open, and a chain that
		// fails it is counted like a broken one: the record is cryptographically
		// intact, so the service can still serve it, but this command's answer
		// is whether the history is one the project stands behind, and a trend
		// drawn through a backwards step is not.
		records, err := store.All(ctx, c)
		if err != nil {
			fmt.Printf("FAIL %s: reading the chain: %v\n", c, err)
			failed++
			continue
		}
		violations := runstore.OrderViolations(records)
		if len(violations) > 0 {
			for _, v := range violations {
				fmt.Printf("FAIL %s: %v\n", c, v)
			}
			failed++
			continue
		}
		fmt.Printf("ok   %s: %d records, latest %s\n",
			c, latest.Seq, latest.RecordedAt.UTC().Format(time.RFC3339))
	}
	if failed > 0 {
		fmt.Printf("\n%d of %d chains failed verification\n", failed, len(corridors))
		return 1
	}
	return 0
}

// rotateStore trims every corridor chain over the record ceiling to its newest
// ceiling records, re-sealing the surviving window so it still verifies.
//
// This is the mechanism behind the decision recorded in ADR 007 and enforced
// by the measure workflow: the committed `data/` is a bounded window, and a
// chain that has outgrown it is rotated rather than left to grow forever.
// Chains under the ceiling are untouched; the ceiling is the default
// runstore.MaxWindow unless an operator overrides it with -rotate-records.
func rotateStore(store runstore.Store, ceiling int, logger *slog.Logger) int {
	fs, ok := store.(*runstore.FileStore)
	if !ok {
		logger.Error("-rotate-store needs a data directory; none is configured")
		return 2
	}

	ctx := context.Background()
	rotations, err := fs.Rotate(ctx, ceiling)
	if err != nil {
		logger.Error("rotating store", "error", err)
		return 1
	}
	if len(rotations) == 0 {
		fmt.Println("no corridor exceeds the window ceiling")
		return 0
	}
	for _, r := range rotations {
		fmt.Printf("rotated %s: %d -> %d records (dropped seq %d..%d), new head seq %d\n",
			r.Corridor, r.RecordsBefore, r.RecordsAfter, r.DroppedSeqStart, r.DroppedSeqEnd, r.NewHeadSeq)
	}
	return 0
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l}))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// progName labels the -version line with the binary that printed it. It is a
// constant rather than os.Args[0] because a renamed or packaged copy should not
// print a name a reader cannot match to a release.
const progName = "wayfared"

// printVersion writes the one-line build provenance (issue #707). It takes the
// writer so the line is checkable without running main.
func printVersion(w io.Writer, prog string) {
	fmt.Fprintln(w, buildinfo.Get().Line(prog))
}

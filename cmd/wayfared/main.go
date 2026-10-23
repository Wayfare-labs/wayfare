// Command wayfared serves the Wayfare corridor monitor and runs its scheduler.
//
//	wayfared                          # serve on :8080 and measure every 6h
//	wayfared -serve=false             # scheduler only, no HTTP
//	wayfared -schedule=0              # server only, no scheduled measurement
//	wayfared -once                    # one sweep, record, exit (CI schedules this)
//	wayfared -verify-store            # walk the hash chains and exit
//	wayfared -export USDC-NGNC        # export one verified chain as CSV and exit
//	wayfared -rotate-store            # trim chains over the window ceiling and exit
//	wayfared -check-fresh             # exit non-zero if any record is stale
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
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Wayfare-labs/wayfare"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/monitor"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/server"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

func main() {
	var (
		addr    = flag.String("addr", defaultAddr(), "listen address (default follows $PORT when set)")
		horizon = flag.String("horizon", "", "Horizon base URL (default: mainnet)")
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
		rotate    = flag.Bool("rotate-store", false, "trim every chain over the window ceiling and exit")
		rotateRec = flag.Int("rotate-records", runstore.MaxWindow,
			"per-corridor record ceiling for -rotate-store")
		once     = flag.Bool("once", false, "measure every corridor once, record, and exit")
		exportTo = flag.String("export", "",
			"export one corridor's verified history to stdout and exit, e.g. -export USDC-NGNC; requires -format")
		exportFmt = flag.String("format", exportFormatCSV,
			"export format for -export; csv is the only implemented format")
		checkFresh = flag.Bool("check-fresh", false,
			"report corridors whose newest record is older than -max-age (or missing) and exit; 1 stale, 2 check unavailable")
		maxAge = flag.Duration("max-age", 2*monitor.DefaultInterval,
			"staleness ceiling for -check-fresh; default is two missed sweeps")
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
		logLevel = flag.String("log-level", envOr("WAYFARE_LOG_LEVEL", "info"), "debug, info, warn or error")
	)
	flag.Parse()

	logger := newLogger(*logLevel)
	slog.SetDefault(logger)

	store, err := openStore(*dataDir, logger)
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

	engine := &route.Engine{
		DEX: &dex.Client{HorizonURL: *horizon, Logger: logger},
		// Two independent providers, each cached, cross-checked for
		// divergence. Caching matters here specifically: a ladder is a
		// dozen quotes that would otherwise each fetch the same mid.
		RefRate: &refrate.Cross{
			Primary:   &refrate.Cached{Inner: &refrate.ExchangeRateAPI{Logger: logger}},
			Secondary: &refrate.Cached{Inner: &refrate.CurrencyAPI{Logger: logger}},
			// The third provider is consulted only when the first two
			// disagree, and only to attribute which of them is the outlier.
			// It never contributes a scored mid (see refrate.Cross). Cached
			// because a sweep prices many rungs and a diagnostic is a
			// daily-published figure, not a per-rung one.
			Diagnostic: &refrate.Cached{Inner: &refrate.FloatRates{Logger: logger}},
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One sweep and exit. This is how a scheduled CI job drives the monitor:
	// the runner is the clock, so the process does not need to be long-lived,
	// and the chain it appends to is the same one a hosted instance would
	// write. Nothing about the measurement differs.
	if *once {
		sched := newScheduler(engine, store, *dataDir, *horizon, logger)
		if err := sched.RunOnce(ctx); err != nil {
			logger.Error("sweep failed", "error", err)
			os.Exit(1)
		}
		return
	}

	var wg sync.WaitGroup

	if *schedule > 0 {
		sched := newScheduler(engine, store, *dataDir, *horizon, logger)
		sched.Interval = *schedule
		sched.Jitter = *scheduleJitter
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sched.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("scheduler stopped", "error", err)
			}
		}()
	} else {
		logger.Warn("scheduler disabled; history will only grow if another instance is writing")
	}

	if *serve {
		srv := &server.Server{
			Engine:       engine,
			Store:        store,
			Timeout:      *timeout,
			HistoryFirst: *histFirst,
			Checks:       &checks.Runner{HorizonURL: *horizon},
			// Nil when limiting is disabled (rate 0 or burst 0): the field is
			// optional on Server, and a disabled limiter should be absent
			// rather than present and inert.
			Limiter: server.NewRateLimiter(*rateLimit, *rateBurst),
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

func newScheduler(engine *route.Engine, store runstore.Store, dataDir, horizon string,
	logger *slog.Logger) *monitor.Scheduler {
	sched := &monitor.Scheduler{
		Engine: engine, Store: store, Logger: logger,
		Checks: &checks.Runner{HorizonURL: horizon},
	}
	if dataDir == "" {
		return sched
	}

	sched.SnapshotDir = filepath.Join(dataDir, "snapshots")
	horizonURL := horizon
	if horizonURL == "" {
		horizonURL = dex.DefaultHorizonURL
	}
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
	sched.SnapshotEngineFactory = func(recorder *snapshot.Recorder) *route.Engine {
		recorder.Transport = guardedTransport
		client := &http.Client{Transport: recorder, Timeout: 30 * time.Second}
		return &route.Engine{
			DEX: &dex.Client{HorizonURL: horizon, HTTPClient: client, Logger: logger},
			RefRate: &refrate.Cross{
				Primary: &refrate.Cached{Inner: &refrate.ExchangeRateAPI{Client: client, Logger: logger}},
				Secondary: &refrate.Cached{
					Inner: &refrate.CurrencyAPI{Client: client, Logger: logger},
				},
				Diagnostic: &refrate.Cached{Inner: &refrate.FloatRates{Client: client, Logger: logger}},
			},
		}
	}
	sched.SnapshotChecksFactory = func(recorder *snapshot.Recorder) *checks.Runner {
		return &checks.Runner{
			HorizonURL: horizon,
			HTTPClient: &http.Client{Transport: recorder, Timeout: 30 * time.Second},
		}
	}
	return sched
}

// openStore opens the run store, or returns a no-op store when history is off.
//
// A store that fails to open is fatal rather than silently degraded: on a
// deployment with a volume attached, a failure here means the volume did not
// mount, and running anyway would record nothing while appearing healthy.
func openStore(dir string, logger *slog.Logger) (runstore.Store, error) {
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
			return runstore.Nop{}, nil
		}
		corridors, _ := embedded.Corridors(context.Background())
		if len(corridors) == 0 {
			logger.Warn("embedded history is empty; measurements will not be recorded")
			return runstore.Nop{}, nil
		}
		logger.Info("serving embedded history (read-only)",
			"corridors", len(corridors))
		return embedded, nil
	}
	store, err := runstore.Open(dir)
	if err != nil {
		return nil, err
	}
	corridors, _ := store.Corridors(context.Background())
	logger.Info("run store open", "dir", dir, "corridors", len(corridors))
	return store, nil
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

// Command wayfared serves the Wayfare corridor monitor and runs its scheduler.
//
//	wayfared                          # serve on :8080 and measure every 6h
//	wayfared -serve=false             # scheduler only, no HTTP
//	wayfared -schedule=0              # server only, no scheduled measurement
//	wayfared -once                    # one sweep, record, exit (CI schedules this)
//	wayfared -verify-store            # walk the hash chains and exit
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
	"sync"
	"syscall"
	"time"

	"github.com/Wayfare-labs/wayfare"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/monitor"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/server"
)

func main() {
	var (
		addr      = flag.String("addr", ":8080", "listen address")
		horizon   = flag.String("horizon", "", "Horizon base URL (default: mainnet)")
		timeout   = flag.Duration("timeout", 90*time.Second, "per-corridor measurement timeout")
		dataDir   = flag.String("data", envOr("WAYFARE_DATA_DIR", ""), "directory for the run store; empty disables history")
		schedule  = flag.Duration("schedule", monitor.DefaultInterval, "measurement interval; 0 disables the scheduler")
		serve     = flag.Bool("serve", true, "serve HTTP")
		verify    = flag.Bool("verify-store", false, "verify every corridor chain and exit")
		once      = flag.Bool("once", false, "measure every corridor once, record, and exit")
		histFirst = flag.Bool("history-first", false,
			"serve the stored run instead of measuring, unless a request asks for ?live=1")
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
		os.Exit(verifyStore(store, logger))
	}

	if !*once && !*serve && *schedule == 0 {
		logger.Error("nothing to do: -serve=false with -schedule=0")
		os.Exit(2)
	}

	engine := &route.Engine{
		DEX: &dex.Client{HorizonURL: *horizon},
		// Two independent providers, each cached, cross-checked for
		// divergence. Caching matters here specifically: a ladder is a
		// dozen quotes that would otherwise each fetch the same mid.
		RefRate: &refrate.Cross{
			Primary:   &refrate.Cached{Inner: &refrate.ExchangeRateAPI{}},
			Secondary: &refrate.Cached{Inner: &refrate.CurrencyAPI{}},
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// One sweep and exit. This is how a scheduled CI job drives the monitor:
	// the runner is the clock, so the process does not need to be long-lived,
	// and the chain it appends to is the same one a hosted instance would
	// write. Nothing about the measurement differs.
	if *once {
		sched := &monitor.Scheduler{Engine: engine, Store: store, Logger: logger}
		if err := sched.RunOnce(ctx); err != nil {
			logger.Error("sweep failed", "error", err)
			os.Exit(1)
		}
		return
	}

	var wg sync.WaitGroup

	if *schedule > 0 {
		sched := &monitor.Scheduler{
			Engine:   engine,
			Store:    store,
			Interval: *schedule,
			Logger:   logger,
		}
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
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(shutdownCtx); err != nil {
				logger.Error("shutdown", "error", err)
			}
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
func verifyStore(store runstore.Store, logger *slog.Logger) int {
	fs, ok := store.(*runstore.FileStore)
	if !ok {
		logger.Error("-verify-store needs a data directory; none is configured")
		return 2
	}

	ctx := context.Background()
	corridors, err := fs.Corridors(ctx)
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
		if err := fs.Verify(ctx, c); err != nil {
			fmt.Printf("FAIL %s: %v\n", c, err)
			failed++
			continue
		}
		latest, _ := fs.Latest(ctx, c)
		fmt.Printf("ok   %s: %d records, latest %s\n",
			c, latest.Seq, latest.RecordedAt.UTC().Format(time.RFC3339))
	}
	if failed > 0 {
		fmt.Printf("\n%d of %d chains failed verification\n", failed, len(corridors))
		return 1
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

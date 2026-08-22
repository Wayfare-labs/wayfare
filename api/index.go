// Package handler is the Vercel serverless entrypoint.
//
// Vercel's Go runtime builds every .go file in this directory as its own
// function, so there is exactly one file and it delegates everything to
// server.Server. The routing, the wire shape and the honesty rules all live
// there; nothing about the deployment target is allowed to change them.
//
// # Why this deployment serves history first
//
// A serverless function cannot hold a request open long enough to price a
// twelve-rung ladder across three corridors, and it has no writable disk to
// record one. So the division is:
//
//	writes   the measure workflow, every six hours, committing the chain
//	reads    this function, serving the committed chain
//
// The chain is embedded rather than read from disk. Vercel's filesystem is
// read-only and its contents are not guaranteed, while an embedded chain is
// part of the binary — and it is verified at startup, so a tampered record
// fails the deployment rather than being served.
package handler

import (
	"log"
	"net/http"
	"sync"

	"github.com/Wayfare-labs/wayfare"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/server"
)

var (
	once    sync.Once
	handler http.Handler
)

// build assembles the server once per cold start.
func build() {
	store, err := runstore.OpenFS(wayfare.History, "data")
	if err != nil {
		// A chain that does not verify must not be served. Failing the
		// whole function is the correct outcome: serving records whose
		// integrity is unproven would undo the reason the chain exists.
		log.Printf("embedded history failed to load: %v", err)
		handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "measurement history failed verification; refusing to serve it",
				http.StatusInternalServerError)
		})
		return
	}

	srv := &server.Server{
		Engine: &route.Engine{
			DEX: &dex.Client{},
			RefRate: &refrate.Cross{
				Primary:   &refrate.Cached{Inner: &refrate.ExchangeRateAPI{}},
				Secondary: &refrate.Cached{Inner: &refrate.CurrencyAPI{}},
			},
		},
		Store: store,

		// Serve the committed chain by default; ?live=1 measures now and
		// may exceed the function's time limit, which is the caller's
		// choice to make.
		HistoryFirst: true,
	}
	handler = srv.Handler()
}

// Handler is the entrypoint Vercel invokes.
func Handler(w http.ResponseWriter, r *http.Request) {
	once.Do(build)
	handler.ServeHTTP(w, r)
}

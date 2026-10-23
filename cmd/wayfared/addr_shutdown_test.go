package main

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestDefaultAddrFollowsPort(t *testing.T) {
	t.Setenv("PORT", "")
	if got := defaultAddr(); got != ":8080" {
		t.Errorf("defaultAddr() with no PORT = %q, want :8080", got)
	}
	t.Setenv("PORT", "10000")
	if got := defaultAddr(); got != "0.0.0.0:10000" {
		t.Errorf("defaultAddr() with PORT=10000 = %q, want 0.0.0.0:10000", got)
	}
}

// TestShutdownWaitsForInFlightRequest proves SIGTERM handling lets a slow
// request finish rather than cutting it off.
func TestShutdownWaitsForInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("done"))
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()

	type result struct {
		body string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		done <- result{string(b), err}
	}()

	<-started
	shutdownServer(srv, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := <-done
	if r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request = (%q, %v), want it to complete with \"done\"", r.body, r.err)
	}
}

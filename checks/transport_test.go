package checks

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestDisallowedIPRejectsPrivateAndLocalRanges(t *testing.T) {
	for _, raw := range []string{
		"127.0.0.1", "10.0.0.8", "172.16.2.4", "192.168.1.20",
		"169.254.169.254", "::1", "fc00::1", "ff02::1",
	} {
		if !disallowedIP(net.ParseIP(raw)) {
			t.Errorf("disallowedIP(%q) = false", raw)
		}
	}
}

func TestDisallowedIPAllowsPublicAddress(t *testing.T) {
	if disallowedIP(net.ParseIP("8.8.8.8")) {
		t.Error("public address was rejected")
	}
}

func TestGuardedControlRejectsAdversarialAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1:443", "192.168.0.1:80", "[fd00::1]:443"} {
		if err := guardedControl("tcp", address, syscall.RawConn(nil)); err == nil {
			t.Errorf("guardedControl(%q) accepted a private destination", address)
		}
	}
}

func TestDNSRebindingRefusedAtConnectTime(t *testing.T) {
	// A local UDP listener acts as a simulated DNS server for rebinding.
	pc, err := (&net.ListenConfig{}).ListenPacket(context.Background(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket failed: %v", err)
	}
	defer pc.Close()

	var queries int32
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, rerr := pc.ReadFrom(buf)
			if rerr != nil {
				return
			}
			if n < 12 {
				continue
			}
			q := buf[:n]
			count := atomic.AddInt32(&queries, 1)
			qEnd := 12
			for qEnd < n && q[qEnd] != 0 {
				qEnd += int(q[qEnd]) + 1
			}
			qEnd += 1 + 4
			if qEnd > n {
				continue
			}
			resp := make([]byte, 0, 512)
			resp = append(resp, q[0], q[1])             // Transaction ID
			resp = append(resp, 0x81, 0x80)             // Flags: standard response, no error
			resp = append(resp, 0x00, 0x01)             // QDCOUNT = 1
			resp = append(resp, 0x00, 0x01)             // ANCOUNT = 1
			resp = append(resp, 0x00, 0x00)             // NSCOUNT = 0
			resp = append(resp, 0x00, 0x00)             // ARCOUNT = 0
			resp = append(resp, q[12:qEnd]...)          // Question section
			resp = append(resp, 0xc0, 0x0c)             // Pointer to name in question
			resp = append(resp, 0x00, 0x01, 0x00, 0x01) // Type A, Class IN
			resp = append(resp, 0x00, 0x00, 0x00, 0x00) // TTL 0
			resp = append(resp, 0x00, 0x04)             // Length 4
			if count == 1 {
				// Query 1 (check time): resolves to a benign public IP
				resp = append(resp, 93, 184, 216, 34) // 93.184.216.34 (public)
			} else {
				// Query 2+ (connect time): rebinds to an internal/loopback IP
				resp = append(resp, 127, 0, 0, 1) // 127.0.0.1 (loopback)
			}
			_, _ = pc.WriteTo(resp, addr)
		}
	}()

	dnsAddr := pc.LocalAddr().String()
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", dnsAddr)
		},
	}

	// 1. Check time: initial resolution sees a public IP and passes address validation.
	ips, err := resolver.LookupIP(context.Background(), "ip4", "rebind.example.test")
	if err != nil {
		t.Fatalf("check-time resolution failed: %v", err)
	}
	if len(ips) == 0 || disallowedIP(ips[0]) {
		t.Fatalf("check-time expected public IP, got: %v", ips)
	}

	// 2. Connect time: guarded client connects to the same hostname, but DNS rebinds to loopback.
	client := guardedClient(3*time.Second, resolver)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://rebind.example.test/latest/meta-data/", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("guarded client allowed connection to rebound address; expected refusal")
	}
	if !strings.Contains(err.Error(), "refusing to") {
		t.Fatalf("expected refusal error from guard, got: %v", err)
	}
}

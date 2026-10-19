package checks

import (
	"net"
	"syscall"
	"testing"
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

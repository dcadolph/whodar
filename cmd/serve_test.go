package cmd

import (
	"fmt"
	"testing"
)

// TestLoopbackAddr verifies which listen addresses count as loopback-only.
func TestLoopbackAddr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		In   string
		Want bool
	}{
		{In: "127.0.0.1:8765", Want: true},  // Test 0: IPv4 loopback.
		{In: "localhost:8765", Want: true},  // Test 1: Localhost by name.
		{In: "[::1]:8765", Want: true},      // Test 2: IPv6 loopback.
		{In: "0.0.0.0:8765", Want: false},   // Test 3: Every interface.
		{In: ":8765", Want: false},          // Test 4: Empty host binds everything.
		{In: "10.0.0.5:8765", Want: false},  // Test 5: A LAN address.
		{In: "host.corp:8765", Want: false}, // Test 6: A non-loopback hostname.
	}
	for testNum, test := range tests {
		t.Run(fmt.Sprintf("test %d", testNum), func(t *testing.T) {
			t.Parallel()
			if got := loopbackAddr(test.In); got != test.Want {
				t.Errorf("test %d: loopbackAddr(%q) = %t, want %t", testNum, test.In, got, test.Want)
			}
		})
	}
}

package relaytest

import (
	"testing"
	"time"
)

// Timeout bounds every wait in the harness. It is generous so a loaded CI
// machine does not flake, and finite so a real hang fails the test instead of
// stalling it.
const Timeout = 20 * time.Second

// Eventually polls cond until it returns true or Timeout passes, failing the
// test with what. It is for observing state that has no channel to wait on
// (for example "the relay finished rebinding"); the harness itself
// synchronises with channels.
func Eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	if !poll(Timeout, cond) {
		t.Fatalf("timed out waiting for %s", what)
	}
}

func poll(d time.Duration, cond func() bool) bool {
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if cond() {
			return true
		}
		select {
		case <-deadline.C:
			return cond()
		case <-tick.C:
		}
	}
}

// timeoutC is a fresh Timeout-long deadline channel.
func timeoutC() <-chan time.Time { return time.After(Timeout) }

package client

import (
	"testing"
	"time"

	"network-router/daemon"
)

// Route operations wait behind each other in the daemon, so they need the
// long timeout. Status stays short.
func TestRequestTimeout(t *testing.T) {
	c := NewClient()
	for _, action := range []string{daemon.ActionApply, daemon.ActionClear, daemon.ActionRestart, daemon.ActionRefresh} {
		if got := c.requestTimeout(action); got != 120*time.Second {
			t.Errorf("%s: timeout=%v, want 120s", action, got)
		}
	}
	if got := c.requestTimeout(daemon.ActionStatus); got != 5*time.Second {
		t.Errorf("status: timeout=%v, want 5s", got)
	}
}

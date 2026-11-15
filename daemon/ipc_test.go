package daemon

import (
	"strings"
	"testing"
)

// allActions lists every action the IPC contract defines (spec §10.1).
var allActions = []string{ActionStatus, ActionOn, ActionOff, ActionRefresh}

func TestProcessRequestHandlesEveryAction(t *testing.T) {
	server := NewIPCServer(newCoordinatorFixture(t).coordinator)
	for _, action := range allActions {
		resp := server.processRequest(IPCRequest{Action: action})
		if strings.HasPrefix(resp.Message, "Unknown action") {
			t.Errorf("%s: not handled", action)
		}
	}
	if resp := server.processRequest(IPCRequest{Action: "nope"}); resp.Success {
		t.Error("unknown action reported success")
	}
}

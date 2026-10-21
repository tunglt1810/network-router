# Fix the route self-destruct loop after `apply` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `apply` arms auto-routing. The daemon applies routes immediately when the path is ready. When the path is not ready, the daemon waits. The daemon no longer removes routes 2–5 seconds after `apply`.

**Architecture:** `Coordinator.ForceApply()` sets `autoRoutingEnabled = true`. It then reads the snapshot from `SnapshotStore`. When the snapshot is ready, it applies routes. When it is not, it returns "waiting" and `handleSnapshot()` applies routes later. An operation mutex (`opMu`) serializes IPC commands and `handleSnapshot()`, so the two goroutines never apply and clear routes at the same time.

**Tech Stack:** Go, `testing`, `go test -race`.

**Spec:** `docs/specs/network-router-project-spec.md` (sections 6, 10, 12). Root cause: `docs/incident_report_routing_loop.md`.

### Root cause

1. `ForceClear()` sets `autoRoutingEnabled = false`.
2. `ForceApply()` installs routes but does not set `autoRoutingEnabled` back to `true`.
3. Within 5 seconds, the network detector emits a snapshot. `handleSnapshot()` sees `!autoRouting && routesApplied` and calls `ForceClear()`.
4. `restart` (= `clear` + `apply`) always hits the same loop.
5. IPC commands run on their own goroutines. `handleSnapshot()` runs on the coordinator goroutine. Nothing serializes them, so `ApplyRoutes()` and `applier.Clear()` can interleave.

### Approved decisions

1. `apply` always sets `autoRoutingEnabled = true`.
2. Ready snapshot (`WifiActive && PhoneReady`): apply routes immediately and start the DNS proxy.
3. Snapshot not ready, or no snapshot yet: apply nothing and return no error. The daemon returns a "waiting" message. `handleSnapshot()` applies routes when tethering is ready.
4. `clear` is unchanged: it removes routes and disables auto-routing.
5. `disable` is unchanged: it disables auto-routing, and the next snapshot removes routes.
6. `restart` (= `clear` + `apply`) works because of decision 1.
7. The tray icon keeps the condition `autoRouting && routesApplied`. Proposal 4.2.3 of the incident report is rejected.
8. Proposal 4.2.2 of the incident report is rejected. The "auto-routing off → remove routes" branch in `handleSnapshot()` stays.

## Global Constraints

- English client-facing messages follow ASD-STE100 Strict.
- Change only `daemon/coordinator.go`, `daemon/ipc.go`, `daemon/coordinator_test.go`, `tray/tray.go`.
- Add no dependency.
- Put temporary files in the repo's `.tmp/`. Do not use `/tmp`.

## Review Focus

1. `apply` before any snapshot exists (daemon just started): returns "waiting", no panic, no error. → `TestForceApplyWaitsWhenNoSnapshot` (Task 1).
2. Two `apply` calls in a row while routes exist: routes still exist after the next snapshot. → assertion in `TestForceApplyAfterClearKeepsRoutes` (Task 1).
3. `disable` after `apply`: the next snapshot still removes routes. → `TestDisableStillClearsRoutesOnNextSnapshot` (Task 1).
4. IPC commands and `handleSnapshot()` run concurrently: `RoutesApplied` matches the real route count. → `TestCoordinatorSerializesConcurrentOperations` (Task 2).
5. `apply` fails (`ApplyRoutes` returns an error) while the path is ready: auto-routing stays on, `RoutesApplied = false`, the DNS proxy does not start. → `TestForceApplyFailureKeepsAutoRoutingOn` (Task 1).

---

### Task 1: `apply` arms auto-routing

**Files:**
- Modify: `daemon/coordinator.go` (`ForceApply`)
- Modify: `daemon/ipc.go` (`ActionApply`, `ActionRestart`)
- Modify: `tray/tray.go` (`handleApply`)
- Test: `daemon/coordinator_test.go`

**Interfaces:**
- Produces: `func (c *Coordinator) ForceApply() (applied bool, err error)`. `applied = true` when routes were installed. `applied = false, err = nil` when the daemon waits for tethering.

- [ ] **Step 1: Write the failing tests**

Use the existing `newCoordinatorFixture` and `readySnapshot`. Add `notReadySnapshot()` with `WifiActive: true, PhoneReady: false`.

- `TestForceApplyAfterClearKeepsRoutes`: store ready snapshot, `ForceClear()`, `ForceApply()` → `applied == true`, `AutoRoutingEnabled == true`. Then `handleSnapshot(ready)` → `RoutesApplied == true` and `deletedCount()` unchanged. Then a second `ForceApply()` + `handleSnapshot(ready)` → `RoutesApplied == true`.
- `TestForceApplyWaitsWhenTetheringNotReady`: `ForceClear()`, store not-ready snapshot, `ForceApply()` → `applied == false, err == nil`, `AutoRoutingEnabled == true`, `RoutesApplied == false`, DNS starts `== 0`, no added routes. Then `handleSnapshot(ready)` → `RoutesApplied == true`.
- `TestForceApplyWaitsWhenNoSnapshot`: fresh fixture, `ForceApply()` → `applied == false, err == nil`, `AutoRoutingEnabled == true`.
- `TestForceApplyFailureKeepsAutoRoutingOn`: ready snapshot, `manager.addErr` set, `ForceClear()`, `ForceApply()` → `err != nil`, `AutoRoutingEnabled == true`, `RoutesApplied == false`, DNS starts `== 0`.
- `TestDisableStillClearsRoutesOnNextSnapshot`: `ForceApply()` with ready snapshot, `SetAutoRouting(false)`, `handleSnapshot(ready)` → `RoutesApplied == false`, `deletedCount() > 0`.

- [ ] **Step 2: Run the tests and confirm they fail**

Run: `go test ./daemon/ -run 'TestForceApply|TestDisableStill' -v`
Expected: build failure, because `ForceApply` does not return 2 values yet.

- [ ] **Step 3: Change `ForceApply() (bool, error)` in `daemon/coordinator.go`**

1. Lock `c.mu`, set `c.autoRoutingEnabled = true`, unlock. When the old value was `false`, log `"✓ Auto-routing enabled by Apply"`.
2. `snapshot := c.snapshots.Load()`. When `snapshot == nil || !(snapshot.WifiActive && snapshot.PhoneReady)`, log `"Apply armed: waiting for tethering path (%s)"` with `snapshot.PhoneReason` (empty when nil) and `return false, nil`.
3. Keep the rest: `ApplyRoutes()` → `setRoutesApplied(true)` → `startManagedServices()` → `return true, nil`. On error, `return false, err`.

- [ ] **Step 4: Update the callers in `daemon/ipc.go`**

- `ActionApply`: keep the old error message. `applied == true` → `"Routes applied successfully"`. `applied == false` → `"Auto-routing is on. The daemon applies routes when tethering is ready."`.
- `ActionRestart`: same waiting message. Keep `"Routes restarted successfully"` on success.

- [ ] **Step 5: Update `handleApply` in `tray/tray.go`**

The tray shows "Routes applied" even when `resp.Success == false`. Change it:
- Transport error: unchanged.
- `!resp.Success`: `showNotification("Error", resp.Message)`.
- Otherwise: `showNotification("Success", resp.Message)`, then `time.AfterFunc(...)` as before.

- [ ] **Step 6: Run all tests and build**

Run: `go build ./... && go test ./daemon/ -v`
Expected: PASS, 4 old and 5 new tests.

- [ ] **Step 7: Commit**

```bash
git add daemon/coordinator.go daemon/ipc.go daemon/coordinator_test.go tray/tray.go
git commit -m "fix(daemon): make apply enable auto-routing and wait for tethering"
```

---

### Task 2: Serialize route operations

**Files:**
- Modify: `daemon/coordinator.go`
- Test: `daemon/coordinator_test.go`

**Interfaces:**
- Consumes: `ForceApply() (bool, error)` from Task 1.
- Produces: field `opMu sync.Mutex` on `Coordinator`. Internal `clearLocked() error` holds the current body of `ForceClear`. The caller must hold `opMu`.

- [ ] **Step 1: Write the failing test**

`TestCoordinatorSerializesConcurrentOperations`: store ready snapshot. Start 4 goroutines × 50 loops: `ForceApply()`, `ForceClear()`, `handleSnapshot(ready)`, `ForceRefresh()` (ignore errors). After `wg.Wait()`, assert `GetStatus().RoutesApplied == (GetStatus().ActiveRoutes > 0)`.

- [ ] **Step 2: Run the test and confirm it fails or the race detector reports**

Run: `go test -race ./daemon/ -run TestCoordinatorSerializesConcurrentOperations -count=5 -v`
Expected: FAIL from state drift, or `WARNING: DATA RACE`. If it still passes in 5 runs, record that and continue. The test is a regression net. The mutex fixes a defect found by reading the code.

- [ ] **Step 3: Add `opMu` to `daemon/coordinator.go`**

- Add `opMu sync.Mutex` to `Coordinator`, separate from `mu`. `mu` guards fields. `opMu` guards operation sequences.
- `handleSnapshot`, `ForceApply`, `ForceClear`, `ForceRefresh`: first line is `c.opMu.Lock(); defer c.opMu.Unlock()`.
- Move the body of `ForceClear` to `clearLocked()`. `ForceClear` = lock + `clearLocked()`. In `handleSnapshot`, replace `c.ForceClear()` with `c.clearLocked()` to prevent a deadlock.
- `performRefresh` calls `ForceRefresh` and stays unchanged.

- [ ] **Step 4: Run all tests with the race detector**

Run: `go test -race ./... -count=3`
Expected: PASS and no `DATA RACE`.

- [ ] **Step 5: Commit**

```bash
git add daemon/coordinator.go daemon/coordinator_test.go
git commit -m "fix(daemon): serialize route operations between IPC and network events"
```

---

### Final manual check

1. `./build/network-router clear`, then `./build/network-router apply`. Wait 15 seconds. The log has no `Auto-routing disabled, clearing existing routes...` line. The tray icon is green.
2. Unplug the iPhone, then run `apply`. The CLI prints `Auto-routing is on. The daemon applies routes when tethering is ready.`. Plug the iPhone back in. Routes are applied within 5 seconds.
3. `./build/network-router restart`. Routes still exist after 15 seconds.

---

## Outcome

Both tasks were implemented as planned, with these changes:

- **Task 1:** `ForceApply` records `lastPhoneDevice` and `lastPhoneGateway` after it installs routes. Without this, the first snapshot after `apply` saw a false path change and rebound the routes. `TestForceApplyAfterClearKeepsRoutes` caught this.
- **Task 2:** the concurrency test passed before `opMu` existed, because the race window is too narrow to hit. It stays as a smoke test.
- **Final review fixes:**
  - `clear` now uses the 120-second client timeout, because it can wait behind an apply. Test: `TestRequestTimeout` (`client/client_test.go`).
  - A second `apply` keeps the recorded path when routes were already applied, so a real gateway change is still rebound. Test: `TestSecondApplyDoesNotHidePathChange`.
  - `apply` returns the waiting message when fresh discovery inside `ApplyRoutes` returns `ErrPhoneNotReady`. Test: `TestForceApplyWaitsWhenFreshDiscoveryFindsPhoneNotReady`.
- **Deferred:**
  - The apply failure message does not say that auto-routing stays on and retries.
  - The tray `handleClear` and `handleRefresh` ignore `resp.Success`.

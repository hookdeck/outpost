package mqs

import (
	"sync/atomic"
	"testing"
	"time"
)

// The watcher has to reconcile even when the filesystem never tells it
// anything. inotify is local to one kernel, so when accounts_dir is a network
// mount written from another host, no event ever arrives — and the only other
// discovery path is the scan at startup. That combination left a provisioned
// tenant unconsumed for nine days while its stream filled up. The rescan is
// what closes it, so it has to fire on its own.
func TestNATSAccountsWatcher_rescansWithoutFilesystemEvents(t *testing.T) {
	var calls atomic.Int32

	w, err := newNATSAccountsWatcher(t.TempDir(), func() { calls.Add(1) }, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("newNATSAccountsWatcher: %v", err)
	}
	w.start()
	t.Cleanup(w.stop)

	// Nothing touches the directory for the whole of this loop.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected repeated reconciles with no filesystem activity, got %d", calls.Load())
}

// Negative opts out, for a deployment that knows its watch is trustworthy and
// would rather not re-read the directory at all.
func TestNATSAccountsWatcher_rescanCanBeDisabled(t *testing.T) {
	var calls atomic.Int32

	w, err := newNATSAccountsWatcher(t.TempDir(), func() { calls.Add(1) }, -1)
	if err != nil {
		t.Fatalf("newNATSAccountsWatcher: %v", err)
	}
	w.start()
	t.Cleanup(w.stop)

	time.Sleep(200 * time.Millisecond)

	if got := calls.Load(); got != 0 {
		t.Fatalf("expected no reconciles with the rescan disabled, got %d", got)
	}
}

// Zero means "unset", not "never" — the caller passing an unconfigured value
// must not silently end up with discovery on fsnotify alone.
func TestNATSAccountsWatcher_zeroIntervalTakesTheDefault(t *testing.T) {
	w, err := newNATSAccountsWatcher(t.TempDir(), func() {}, 0)
	if err != nil {
		t.Fatalf("newNATSAccountsWatcher: %v", err)
	}
	t.Cleanup(func() { _ = w.w.Close() })

	if w.rescan != defaultRescanInterval {
		t.Fatalf("rescan = %v, want %v", w.rescan, defaultRescanInterval)
	}
}

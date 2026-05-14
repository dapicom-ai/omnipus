package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
)

// ---------------------------------------------------------------------------
// T17 / T17b — unit tests for escapeDetector byte-parser state machine
// ---------------------------------------------------------------------------

// TestRawStdinHandler_DoubleEscapeFiresCancel verifies that feeding two 0x1B
// bytes within the 500ms window produces cancel==true on the second byte.
func TestRawStdinHandler_DoubleEscapeFiresCancel(t *testing.T) {
	d := newEscapeDetector()

	// First Escape: should not cancel, no passThrough.
	cancel, pass := d.feed(0x1B)
	if cancel {
		t.Fatal("first Escape should not trigger cancel")
	}
	if len(pass) != 0 {
		t.Fatalf("first Escape should produce no passThrough; got %v", pass)
	}

	// Second Escape immediately after: should cancel.
	cancel, pass = d.feed(0x1B)
	if !cancel {
		t.Fatal("second Escape within window should trigger cancel")
	}
	if len(pass) != 0 {
		t.Fatalf("cancel event should produce no passThrough; got %v", pass)
	}
}

// TestRawStdinHandler_SingleEscapeNoOp verifies that a single Escape followed
// by 600ms of silence produces no cancel. (T17b)
func TestRawStdinHandler_SingleEscapeNoOp(t *testing.T) {
	d := newEscapeDetector()

	cancel, _ := d.feed(0x1B)
	if cancel {
		t.Fatal("first Escape should not trigger cancel")
	}

	// Simulate 600ms elapsing by rewinding firstEscapeAt into the past.
	// The detector uses wall time from d.firstEscapeAt; we can fake it:
	d.firstEscapeAt = time.Now().Add(-600 * time.Millisecond)

	// Feed an unrelated byte. The expired window should clear pending state
	// and pass the byte through without cancelling.
	cancel, pass := d.feed('a')
	if cancel {
		t.Fatal("single Escape followed by timeout should not cancel")
	}
	if len(pass) != 1 || pass[0] != 'a' {
		t.Fatalf("expected byte 'a' in passThrough; got %v", pass)
	}
}

// TestRawStdinHandler_ArrowKeyNoCancelCSI verifies that ESC + '[' + 'A'
// (Up-arrow CSI sequence) does not trigger cancel and passes the ESC+[ through.
func TestRawStdinHandler_ArrowKeyNoCancelCSI(t *testing.T) {
	d := newEscapeDetector()

	// ESC
	cancel, _ := d.feed(0x1B)
	if cancel {
		t.Fatal("ESC should not cancel")
	}

	// CSI introducer '['
	cancel, pass := d.feed(0x5B)
	if cancel {
		t.Fatal("ESC+[ should not cancel (is a CSI sequence)")
	}
	// passThrough should contain ESC + '[' so readline can handle the sequence.
	if len(pass) != 2 || pass[0] != 0x1B || pass[1] != 0x5B {
		t.Fatalf("expected ESC+'[' in passThrough; got %v", pass)
	}

	// The 'A' byte that completes the Up-arrow sequence: no special handling.
	cancel, pass = d.feed(0x41)
	if cancel {
		t.Fatal("trailing sequence byte should not cancel")
	}
	if len(pass) != 1 || pass[0] != 0x41 {
		t.Fatalf("expected 'A' in passThrough; got %v", pass)
	}
}

// TestRawStdinHandler_SS3SequenceNoCancel verifies that ESC + 'O' + 'P'
// (F1 SS3 sequence) does not trigger cancel.
func TestRawStdinHandler_SS3SequenceNoCancel(t *testing.T) {
	d := newEscapeDetector()

	cancel, _ := d.feed(0x1B)
	if cancel {
		t.Fatal("ESC should not cancel")
	}

	cancel, pass := d.feed(0x4F) // SS3 'O'
	if cancel {
		t.Fatal("ESC+O should not cancel (is an SS3 sequence)")
	}
	if len(pass) != 2 || pass[0] != 0x1B || pass[1] != 0x4F {
		t.Fatalf("expected ESC+'O' in passThrough; got %v", pass)
	}
}

// ---------------------------------------------------------------------------
// T16 — PTY-driven integration test: double-Escape cancels inference
//
// Uses github.com/creack/pty (already in go.mod) to create a real pseudo-
// terminal pair, so startRawStdinWatcher sees an actual TTY fd and term.IsTerminal
// returns true. We replace os.Stdin with the slave side, feed double-Escape via
// the master side, and verify cancelFn is called.
// ---------------------------------------------------------------------------

func TestCli_DoubleEscapeDuringInferenceCancels(t *testing.T) {
	// Open a PTY pair.
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty.Open failed (likely no PTY support in this environment): %v", err)
	}
	defer master.Close()
	defer slave.Close()

	// Replace os.Stdin with the slave side for the duration of this test.
	origStdin := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() {
		os.Stdin = origStdin
	})

	// Create a cancellable context and set up a watcher.
	ctx, cancelFn := context.WithCancel(context.Background())

	stopWatcher, ok := startRawStdinWatcher(ctx, cancelFn)
	if !ok {
		t.Skip("startRawStdinWatcher returned ok=false; stdin is not a TTY in this test environment")
	}
	t.Cleanup(stopWatcher)

	// Give the watcher goroutine a moment to enter its read loop.
	time.Sleep(20 * time.Millisecond)

	// Send double-Escape via the master side.
	if _, writeErr := master.Write([]byte{0x1B, 0x1B}); writeErr != nil {
		t.Fatalf("failed to write double-Escape to PTY master: %v", writeErr)
	}

	// Wait for ctx to be cancelled (watcher fires cancelFn), with a timeout.
	select {
	case <-ctx.Done():
		// Success: cancel was fired.
	case <-time.After(2 * time.Second):
		t.Fatal("context was not cancelled within 2s after double-Escape; cancel may not have fired")
	}
}

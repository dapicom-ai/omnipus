package agent

import (
	"time"
)

// escapeDetector is a byte-level state machine that detects double-Escape (0x1B 0x1B)
// within a 500ms window while ignoring CSI/SS3 escape sequences (arrow keys, F-keys).
//
// Use:
//
//	d := newEscapeDetector()
//	cancel, passThrough := d.feed(b)
//
// When feed returns cancel==true, the caller should fire the cancellation.
// passThrough contains bytes that should be passed to the terminal as-is (e.g.
// the bytes of an arrow-key sequence after we determine it is not a cancel).
//
// The caller drives timing: after receiving a first Escape, it must call
// d.tick() at least once after 500ms to expire the window; alternatively,
// the window expires automatically on the next feed call if enough wall time
// has elapsed.
//
// This type is NOT goroutine-safe; the caller must serialise calls.
type escapeDetector struct {
	// waitingForSecond is true after we have seen a first 0x1B and are
	// waiting to see whether the next byte is another 0x1B (cancel), a CSI/SS3
	// introducer (ignore + pass through), or something else (clear).
	waitingForSecond bool

	// firstEscapeAt is the wall-clock time we received the first 0x1B.
	firstEscapeAt time.Time

	// window is the maximum gap between two consecutive Escape bytes
	// that still counts as "double Escape". Default 500ms.
	window time.Duration

	// csiTimeout is the grace period after 0x1B within which a 0x5B or 0x4F
	// byte is treated as a CSI/SS3 sequence start (not a cancel). Default 50ms.
	csiTimeout time.Duration
}

func newEscapeDetector() *escapeDetector {
	return &escapeDetector{
		window:     500 * time.Millisecond,
		csiTimeout: 50 * time.Millisecond,
	}
}

// feed processes one byte from stdin.
// Returns cancel=true when a double-Escape cancel should be fired.
// Returns passThrough with any bytes that should be treated as regular terminal
// input (e.g. the 0x1B + 0x5B bytes of an arrow key sequence, so readline can
// handle them if needed).
func (d *escapeDetector) feed(b byte) (cancel bool, passThrough []byte) {
	const (
		byteESC = 0x1B
		byteCSI = 0x5B // '[' — CSI introducer after ESC
		byteSS3 = 0x4F // 'O' — SS3 introducer after ESC (F1-F4)
	)

	now := time.Now()

	// If we were waiting for a second Escape but the window has expired,
	// clear the pending state first.
	if d.waitingForSecond && now.Sub(d.firstEscapeAt) > d.window {
		d.waitingForSecond = false
	}

	switch {
	case !d.waitingForSecond && b == byteESC:
		// First Escape: enter the waiting-for-second state.
		d.waitingForSecond = true
		d.firstEscapeAt = now
		return false, nil

	case d.waitingForSecond && b == byteESC:
		// Second Escape within the window → cancel.
		d.waitingForSecond = false
		return true, nil

	case d.waitingForSecond && (b == byteCSI || b == byteSS3):
		// CSI/SS3 introducer within the csiTimeout of the first Escape:
		// this is an arrow key or F-key sequence, not a double-Escape.
		// Pass both the ESC and this byte through so readline can process them.
		d.waitingForSecond = false
		return false, []byte{byteESC, b}

	case d.waitingForSecond:
		// Some other byte after a single Escape: ignore the Escape (no-op)
		// and pass this new byte through normally.
		d.waitingForSecond = false
		return false, []byte{b}

	default:
		// Normal byte with no pending Escape state.
		return false, []byte{b}
	}
}

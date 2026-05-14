package agent

import (
	"context"
	"log/slog"
	"os"

	"golang.org/x/term"
)

// rawStdinWatcher watches stdin in raw mode during agent inference. It polls
// stdin byte-by-byte, feeds each byte to an escapeDetector, and calls cancelFn
// when a double-Escape sequence is detected.
//
// The watcher holds stdin in raw mode for the duration of the watch. Callers
// must:
//  1. Call startRawStdinWatcher to begin watching. It returns (stopFn, ok).
//     If ok==false, the terminal is not a TTY; no watcher is started and
//     stopFn is a no-op.
//  2. Call stopFn (or cancel the returned context) when inference is done.
//     stopFn blocks briefly until the goroutine exits cleanly and restores the
//     terminal to its previous mode.
//
// Coordination with ergochat/readline:
//
//	readline owns stdin ONLY while rl.Readline() is executing. Our watcher owns
//	stdin ONLY while ProcessDirect is executing (between readline.Readline()
//	returning and the next readline.Readline() call). The caller is responsible
//	for enforcing this sequencing.
func startRawStdinWatcher(ctx context.Context, cancelFn context.CancelFunc) (stop func(), ok bool) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		slog.Warn("stdin is not a TTY; double-Escape cancel is unavailable for this session")
		return func() {}, false
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		slog.Warn("failed to set stdin to raw mode; double-Escape cancel is unavailable", "error", err)
		return func() {}, false
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() {
			if restoreErr := term.Restore(fd, oldState); restoreErr != nil {
				slog.Warn("failed to restore terminal state", "error", restoreErr)
			}
		}()

		det := newEscapeDetector()
		buf := make([]byte, 1)

		for {
			// Check whether the caller has signalled us to stop.
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Read one byte. os.File.Read on a raw terminal blocks until a
			// byte is available. When the context is cancelled the caller
			// closes the done channel and we return on the next iteration.
			n, readErr := os.Stdin.Read(buf)
			if readErr != nil || n == 0 {
				// EOF or error — stop watching.
				return
			}

			// Re-check context after potentially blocking read.
			select {
			case <-ctx.Done():
				return
			default:
			}

			cancel, _ := det.feed(buf[0])
			if cancel {
				slog.Info("double-Escape detected; cancelling current inference turn")
				cancelFn()
				// Keep the goroutine alive until ctx.Done() so we don't
				// leave the terminal in raw mode prematurely. The caller
				// will close ctx after ProcessDirect returns.
				<-ctx.Done()
				return
			}
		}
	}()

	stopFn := func() {
		// Signal the goroutine to stop; it will restore terminal state.
		cancelFn()
		<-done
	}
	return stopFn, true
}

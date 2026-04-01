//go:build !cgo

// This test file uses //go:build !cgo so it compiles when CGO is disabled.
// When CGO is enabled, pkg/gateway imports pkg/channels/matrix which requires
// the libolm system library (olm/olm.h). If that library is installed,
// remove this build constraint and run tests normally.

package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/dapicom-ai/omnipus/pkg/bus"
)

// TestWebchatChannel_SendSkipsWhenStreamed verifies that webchatChannel.Send is a no-op
// when the chatID has been marked as streamed via markStreamed.
// BDD: Given a webchatChannel whose chatID "chat-streamed" has been marked as streamed,
// When Send is called with that chatID,
// Then no frame is placed on the wsConn's sendCh (the response is already delivered).
// Traces to: pkg/gateway/webchat_channel.go — webchatChannel.Send alreadyStreamed guard
func TestWebchatChannel_SendSkipsWhenStreamed(t *testing.T) {
	t.Helper()

	handler, _, _ := newTestWSHandler(t)
	wc := makeTestConn()

	chatID := "chat-streamed"

	// Register the connection in the handler's session map so Send can look it up.
	handler.mu.Lock()
	handler.sessions[chatID] = wc
	handler.mu.Unlock()

	ch := newWebchatChannel(handler)

	// Mark the chatID as already streamed — this simulates wsStreamer.Finalize having run.
	ch.markStreamed(chatID)

	// Call Send — it must detect alreadyStreamed and return without writing to sendCh.
	err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  chatID,
		Content: "should not appear",
	})

	// Send returns nil (no error) because the streamed path is a deliberate no-op.
	assert.NoError(t, err, "Send must return nil when response was already streamed")

	// Verify that no frame was placed on sendCh.
	select {
	case frame := <-wc.sendCh:
		t.Fatalf("unexpected frame on sendCh — Send must be a no-op when streamed, got: %s", string(frame))
	case <-time.After(100 * time.Millisecond):
		// Correct — sendCh must remain empty.
	}
}

package bus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/dapicom-ai/omnipus/pkg/logger"
)

// ErrBusClosed is returned when publishing to a closed MessageBus.
var ErrBusClosed = errors.New("message bus closed")

const defaultBusBufferSize = 64

// StreamDelegate is implemented by the channel Manager to provide streaming
// capabilities to the agent loop without tight coupling.
type StreamDelegate interface {
	// GetStreamer returns a Streamer for the given channel+chatID if the channel
	// supports streaming. Returns nil, false if streaming is unavailable.
	GetStreamer(ctx context.Context, channel, chatID string) (Streamer, bool)
}

// Streamer pushes incremental content to a streaming-capable channel.
// Defined here so the agent loop can use it without importing pkg/channels.
type Streamer interface {
	Update(ctx context.Context, content string) error
	Finalize(ctx context.Context, content string) error
	Cancel(ctx context.Context)
}

type MessageBus struct {
	inbound       chan InboundMessage
	outbound      chan OutboundMessage
	outboundMedia chan OutboundMediaMessage

	closeOnce      sync.Once
	done           chan struct{}
	closed         atomic.Bool
	publishMu      sync.Mutex // guards closed+wg.Add to prevent TOCTOU race with Close()
	wg             sync.WaitGroup
	streamDelegate atomic.Pointer[StreamDelegate] // type-safe; avoids atomic.Value mixed-type panic
}

func NewMessageBus() *MessageBus {
	return &MessageBus{
		inbound:       make(chan InboundMessage, defaultBusBufferSize),
		outbound:      make(chan OutboundMessage, defaultBusBufferSize),
		outboundMedia: make(chan OutboundMediaMessage, defaultBusBufferSize),
		done:          make(chan struct{}),
	}
}

func publish[T any](ctx context.Context, mb *MessageBus, ch chan T, msg T) error {
	// Atomically check closed and increment wg under publishMu to prevent a
	// TOCTOU race with Close(): without the lock, Close() could call wg.Wait()
	// and close the channels between our closed check and wg.Add(1), causing a
	// panic when we subsequently try to send to the closed channel.
	mb.publishMu.Lock()
	if mb.closed.Load() {
		mb.publishMu.Unlock()
		return ErrBusClosed
	}
	mb.wg.Add(1)
	mb.publishMu.Unlock()
	defer mb.wg.Done()

	select {
	case ch <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-mb.done:
		return ErrBusClosed
	}
}

func (mb *MessageBus) PublishInbound(ctx context.Context, msg InboundMessage) error {
	return publish(ctx, mb, mb.inbound, msg)
}

func (mb *MessageBus) InboundChan() <-chan InboundMessage {
	return mb.inbound
}

func (mb *MessageBus) PublishOutbound(ctx context.Context, msg OutboundMessage) error {
	return publish(ctx, mb, mb.outbound, msg)
}

func (mb *MessageBus) OutboundChan() <-chan OutboundMessage {
	return mb.outbound
}

func (mb *MessageBus) PublishOutboundMedia(ctx context.Context, msg OutboundMediaMessage) error {
	return publish(ctx, mb, mb.outboundMedia, msg)
}

func (mb *MessageBus) OutboundMediaChan() <-chan OutboundMediaMessage {
	return mb.outboundMedia
}

// SetStreamDelegate registers a StreamDelegate (typically the channel Manager).
func (mb *MessageBus) SetStreamDelegate(d StreamDelegate) {
	mb.streamDelegate.Store(&d)
}

// GetStreamer returns a Streamer for the given channel+chatID via the delegate.
func (mb *MessageBus) GetStreamer(ctx context.Context, channel, chatID string) (Streamer, bool) {
	if dp := mb.streamDelegate.Load(); dp != nil && *dp != nil {
		return (*dp).GetStreamer(ctx, channel, chatID)
	}
	return nil, false
}

func (mb *MessageBus) Close() {
	mb.closeOnce.Do(func() {
		// Hold publishMu while setting done+closed so no new publisher can slip
		// between the closed check and wg.Add(1) after we start wg.Wait().
		mb.publishMu.Lock()
		close(mb.done)
		mb.closed.Store(true)
		mb.publishMu.Unlock()

		// wait for all ongoing Publish calls to finish, ensuring all messages have been sent to channels or exited
		mb.wg.Wait()

		// close channels safely
		close(mb.inbound)
		close(mb.outbound)
		close(mb.outboundMedia)

		// clean up any remaining messages in channels
		drained := 0
		for range mb.inbound {
			drained++
		}
		for range mb.outbound {
			drained++
		}
		for range mb.outboundMedia {
			drained++
		}

		if drained > 0 {
			logger.DebugCF("bus", "Drained buffered messages during close", map[string]any{
				"count": drained,
			})
		}
	})
}

package service

import (
	"context"
	"sync/atomic"
	"time"

	coderws "github.com/coder/websocket"
)

const antiStallWebSocketWriteTimeout = 5 * time.Second

type antiStallWebSocketWriter interface {
	Write(context.Context, coderws.MessageType, []byte) error
}

type antiStallWebSocketPinger interface {
	Ping(context.Context) error
}

// antiStallWSActivity tracks downstream write activity so keepalive only fires
// while the session is otherwise idle.
type antiStallWSActivity struct {
	lastWriteUnixNano atomic.Int64
}

func newAntiStallWSActivity() *antiStallWSActivity {
	a := &antiStallWSActivity{}
	a.Touch()
	return a
}

func (a *antiStallWSActivity) Touch() {
	if a == nil {
		return
	}
	a.lastWriteUnixNano.Store(time.Now().UnixNano())
}

func (a *antiStallWSActivity) IdleFor(d time.Duration) bool {
	if a == nil {
		return true
	}
	last := a.lastWriteUnixNano.Load()
	if last <= 0 {
		return true
	}
	return time.Since(time.Unix(0, last)) >= d
}

// StartAntiStallWebSocketKeepalive emits standard WebSocket Ping control frames
// while an opted-in session is otherwise idle. It never injects application
// data events (response.keepalive) that strict SDK parsers reject.
func StartAntiStallWebSocketKeepalive(ctx context.Context, conn *coderws.Conn) context.CancelFunc {
	keepaliveCtx, cancel := context.WithCancel(ctx)
	if conn == nil {
		cancel()
		return cancel
	}
	activity := newAntiStallWSActivity()
	go runAntiStallWebSocketKeepalive(
		keepaliveCtx,
		conn,
		activity,
		antiStallKeepaliveInterval,
		antiStallWebSocketWriteTimeout,
	)
	return cancel
}

func runAntiStallWebSocketKeepalive(
	ctx context.Context,
	writer antiStallWebSocketWriter,
	activity *antiStallWSActivity,
	interval time.Duration,
	writeTimeout time.Duration,
) {
	if ctx == nil || writer == nil {
		return
	}
	if interval <= 0 {
		interval = antiStallKeepaliveInterval
	}
	if writeTimeout <= 0 {
		writeTimeout = antiStallWebSocketWriteTimeout
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// The ticker value may already be buffered when cancellation happens.
			// Prefer cancellation before processing that stale tick.
			select {
			case <-ctx.Done():
				return
			default:
			}
			// Only ping when the downstream path has been idle for a full interval
			// so active delta streams are never interleaved with control noise.
			if activity != nil && !activity.IdleFor(interval) {
				continue
			}
			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := writeAntiStallWebSocketKeepalive(writeCtx, writer)
			cancel()
			if err != nil {
				return
			}
			if activity != nil {
				activity.Touch()
			}
		}
	}
}

func writeAntiStallWebSocketKeepalive(ctx context.Context, writer antiStallWebSocketWriter) error {
	if pinger, ok := writer.(antiStallWebSocketPinger); ok {
		return pinger.Ping(ctx)
	}
	if conn, ok := writer.(*coderws.Conn); ok && conn != nil {
		return conn.Ping(ctx)
	}
	// No Ping support (tests/fakes): skip application-level fake events entirely.
	return nil
}

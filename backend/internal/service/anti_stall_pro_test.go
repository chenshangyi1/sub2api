package service

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveAntiStallForKey(t *testing.T) {
	admin := DefaultAntiStallAdminConfig()
	admin.ModuleEnabled = true

	off := ResolveAntiStallForKey(admin, "off")
	require.False(t, off.Enabled)

	admin.ModuleEnabled = false
	proOff := ResolveAntiStallForKey(admin, "pro")
	require.False(t, proOff.Enabled)

	admin.ModuleEnabled = true
	basic := ResolveAntiStallForKey(admin, "basic")
	require.True(t, basic.Enabled)
	require.Equal(t, AntiStallTierBasic, basic.Tier)
	require.Equal(t, admin.Basic.BufferTokens, basic.BufferTokens)

	ultra := ResolveAntiStallForKey(admin, "ULTRA")
	require.True(t, ultra.Enabled)
	require.Equal(t, AntiStallTierUltra, ultra.Tier)
}

func TestAntiStallOffer_ReconnectExitsDrip(t *testing.T) {
	s := NewAntiStallSession(AntiStallProSettings{
		Enabled:      true,
		BufferTokens: 10,
	})
	_ = s.Offer([]byte("a"), 1)
	s.BeginRecovery()
	require.Equal(t, 1, s.UpstreamFails())

	// New data while dripping = reconnect detected → exit drip, resume normal.
	_ = s.Offer([]byte("reconnected"), 1)
	require.Equal(t, 0, s.UpstreamFails())
	// Still have reserve (hold-back)
	require.Greater(t, s.ReserveWeight(), 0)
}

func TestAntiStallDripTimeoutForcesSwitch(t *testing.T) {
	s := NewAntiStallSession(AntiStallProSettings{
		Enabled:          true,
		BufferTokens:     10,
		UpstreamMaxRetry: 99, // high so only timeout triggers
		LowBufferTokens:  0,
		MaxDripSeconds:   1,
		MaxLeafSwitches:  3,
	})
	_ = s.Offer([]byte("x"), 1)
	s.BeginRecovery()
	require.False(t, s.ShouldSwitchLeaf(0))

	// Simulate time passing by poking internal state via timeout after sleep.
	time.Sleep(1100 * time.Millisecond)
	require.True(t, s.DripTimedOut())
	require.True(t, s.ShouldSwitchLeaf(0))
}

func TestAntiStallOffer_HoldsBackBuffer(t *testing.T) {
	s := NewAntiStallSession(AntiStallProSettings{
		Enabled:      true,
		BufferTokens: 3,
	})
	var flushed int
	for i := 0; i < 5; i++ {
		out := s.Offer([]byte{byte('a' + i)}, 1)
		flushed += len(out)
	}
	require.Equal(t, 2, flushed)
	require.Equal(t, 3, s.ReserveWeight())
}

func TestAntiStallMaxLeafSwitches(t *testing.T) {
	s := NewAntiStallSession(AntiStallProSettings{
		Enabled:          true,
		BufferTokens:     5,
		UpstreamMaxRetry: 1,
		LowBufferTokens:  5,
		MaxLeafSwitches:  2,
		MaxDripSeconds:   60,
	})
	s.BeginRecovery() // fails=1 → should switch
	require.True(t, s.ShouldSwitchLeaf(0))
	s.RecordLeafSwitch()
	s.BeginRecovery()
	require.True(t, s.ShouldSwitchLeaf(0))
	s.RecordLeafSwitch()
	// switches == 2, no more
	s.BeginRecovery()
	require.False(t, s.ShouldSwitchLeaf(0))
	require.True(t, s.ShouldFailHard())
}

func TestAntiStallKeepalivePayloadIsSSEComment(t *testing.T) {
	frame := string(antiStallKeepalivePayload)
	require.True(t, strings.HasPrefix(frame, ": "), "SSE keepalive must be a comment, not data:")
	require.True(t, strings.HasSuffix(frame, "\n\n"))
	require.NotContains(t, frame, "data:")
	require.NotContains(t, frame, "response.keepalive")
}

func TestAntiStallEmptyReserveWritesKeepaliveWithoutSemanticCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	s := NewAntiStallSession(AntiStallProSettings{
		Enabled:          true,
		BufferTokens:     8,
		MaxDripSeconds:   10,
		MaxLeafSwitches:  3,
		UpstreamMaxRetry: 2,
	})
	cleanup := InstallAntiStallProWriter(c, s)
	defer cleanup()
	wrapped, ok := c.Writer.(*antiStallProGinWriter)
	require.True(t, ok)

	s.BeginRecovery()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAntiStallDripLoop(c, s, stop, 10*time.Millisecond)
	}()

	require.Eventually(t, func() bool {
		return wrapped.transportByteCount() > 0
	}, time.Second, 5*time.Millisecond)
	require.True(t, s.IsDripping(), "keepalive must not masquerade as upstream reconnect data")
	require.Equal(t, -1, c.Writer.Size(), "transport keepalive must not block upstream failover")

	close(stop)
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	require.Contains(t, recorder.Body.String(), string(antiStallKeepalivePayload))
}

func TestAntiStallKeepaliveStopsWhenUpstreamReconnects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	s := NewAntiStallSession(AntiStallProSettings{
		Enabled:         true,
		BufferTokens:    8,
		MaxDripSeconds:  10,
		MaxLeafSwitches: 3,
	})
	cleanup := InstallAntiStallProWriter(c, s)
	defer cleanup()
	wrapped, ok := c.Writer.(*antiStallProGinWriter)
	require.True(t, ok)

	s.BeginRecovery()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAntiStallDripLoop(c, s, stop, 10*time.Millisecond)
	}()
	require.Eventually(t, func() bool {
		return wrapped.transportByteCount() > 0
	}, time.Second, 5*time.Millisecond)

	_, err := c.Writer.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	require.False(t, s.IsDripping())
	require.Contains(t, recorder.Body.String(), string(antiStallKeepalivePayload))
}

type antiStallWebSocketWriterRecorder struct {
	mu    sync.Mutex
	pings int
	err   error
}

func (w *antiStallWebSocketWriterRecorder) Write(context.Context, coderws.MessageType, []byte) error {
	return errors.New("application data keepalive must not be used")
}

func (w *antiStallWebSocketWriterRecorder) Ping(context.Context) error {
	w.mu.Lock()
	w.pings++
	err := w.err
	w.mu.Unlock()
	return err
}

func (w *antiStallWebSocketWriterRecorder) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pings
}

func TestAntiStallWebSocketKeepaliveUsesPingWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	writer := &antiStallWebSocketWriterRecorder{}
	activity := newAntiStallWSActivity()
	// Force idle so the first tick immediately pings.
	activity.lastWriteUnixNano.Store(time.Now().Add(-time.Second).UnixNano())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAntiStallWebSocketKeepalive(ctx, writer, activity, 5*time.Millisecond, time.Second)
	}()

	require.Eventually(t, func() bool { return writer.count() > 0 }, time.Second, 5*time.Millisecond)
	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
}

func TestAntiStallWebSocketKeepaliveSkipsWhenRecentlyActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &antiStallWebSocketWriterRecorder{}
	activity := newAntiStallWSActivity()
	activity.Touch()
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Interval must exceed the observation window so IdleFor(interval)
		// still treats the recent Touch() as active (v247 keepalive).
		runAntiStallWebSocketKeepalive(ctx, writer, activity, time.Second, time.Second)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	require.Equal(t, 0, writer.count())
}

func TestAntiStallWebSocketKeepaliveStopsAfterPingFailure(t *testing.T) {
	writer := &antiStallWebSocketWriterRecorder{err: errors.New("client closed")}
	activity := newAntiStallWSActivity()
	activity.lastWriteUnixNano.Store(time.Now().Add(-time.Second).UnixNano())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAntiStallWebSocketKeepalive(context.Background(), writer, activity, 5*time.Millisecond, time.Second)
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond)
	require.Equal(t, 1, writer.count())
}

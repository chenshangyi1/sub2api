package service

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	ginKeyAntiStallSession     = "anti_stall_pro_session"
	ginKeyAntiStallDripStop    = "anti_stall_pro_drip_stop"
	antiStallKeepaliveInterval = 10 * time.Second
	// SSE comment keepalive: ignored by OpenAI SDK stream parsers (chat and
	// responses) while still flushing the connection so proxies/clients do not
	// idle-timeout during drip recovery. Must not look like a data: event.
	antiStallKeepaliveComment = ": keepalive\n\n"
)

var antiStallKeepalivePayload = []byte(antiStallKeepaliveComment)

// InstallAntiStallProWriter wraps the gin response writer with hold-back
// buffering when Anti-Stall PRO is enabled. Returns a stop func (flush+unwrap).
func InstallAntiStallProWriter(c *gin.Context, session *AntiStallSession) func() {
	if c == nil || c.Writer == nil || session == nil || !session.Config().Enabled {
		return func() {}
	}
	original := c.Writer
	w := &antiStallProGinWriter{
		ResponseWriter: original,
		session:        session,
	}
	c.Set(ginKeyAntiStallSession, session)
	c.Writer = w
	return func() {
		StopAntiStallDrip(c)
		// Flush remaining reserve on successful completion.
		for _, p := range session.FlushAll() {
			_, _ = original.Write(p)
		}
		if fl, ok := original.(http.Flusher); ok {
			fl.Flush()
		}
		if current, ok := c.Writer.(*antiStallProGinWriter); ok && current == w {
			c.Writer = original
		}
	}
}

// AntiStallSessionFromGin returns the session installed for this request.
func AntiStallSessionFromGin(c *gin.Context) *AntiStallSession {
	if c == nil {
		return nil
	}
	if v, ok := c.Get(ginKeyAntiStallSession); ok {
		if s, ok := v.(*AntiStallSession); ok {
			return s
		}
	}
	return nil
}

// EnsureAntiStallDripRunning starts a background drip loop if not already running.
// Drip exits on reconnect (Offer clears dripMode), drip timeout, stop, or client gone.
func EnsureAntiStallDripRunning(c *gin.Context, session *AntiStallSession) {
	if c == nil || session == nil || !session.Config().Enabled {
		return
	}
	// Already running?
	if v, ok := c.Get(ginKeyAntiStallDripStop); ok {
		if ch, ok := v.(chan struct{}); ok && ch != nil {
			select {
			case <-ch:
				// previous stopped; start new
			default:
				return // still running
			}
		}
	}
	stop := make(chan struct{})
	c.Set(ginKeyAntiStallDripStop, stop)
	go RunAntiStallDripLoop(c, session, stop)
}

// StopAntiStallDrip signals the background drip loop to exit.
func StopAntiStallDrip(c *gin.Context) {
	if c == nil {
		return
	}
	if v, ok := c.Get(ginKeyAntiStallDripStop); ok {
		if ch, ok := v.(chan struct{}); ok && ch != nil {
			select {
			case <-ch:
				// already closed
			default:
				close(ch)
			}
		}
	}
	c.Set(ginKeyAntiStallDripStop, nil)
}

// antiStallProGinWriter intercepts Write/WriteString, holds back tokens, and
// can drip them slowly during upstream recovery.
type antiStallProGinWriter struct {
	gin.ResponseWriter
	session *AntiStallSession

	mu             sync.Mutex
	lineBuf        []byte
	transportBytes int
}

func (w *antiStallProGinWriter) writeDownstream(p []byte) error {
	if _, err := w.ResponseWriter.Write(p); err != nil {
		return err
	}
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
	return nil
}

// dripWrite emits one held payload during recovery (bypasses hold-back buffer).
func (w *antiStallProGinWriter) dripWrite(p []byte) error {
	if w == nil || len(p) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writeDownstream(p)
}

// keepaliveWrite emits an SSE comment that keeps the transport alive without
// injecting a parseable data event. Bytes are tracked as transport-only so
// Size() / MarkInFlight fail-closed logic still treats the stream as uncommitted.
func (w *antiStallProGinWriter) keepaliveWrite() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.ResponseWriter.Write(antiStallKeepalivePayload)
	w.transportBytes += n
	if err == nil {
		if fl, ok := w.ResponseWriter.(http.Flusher); ok {
			fl.Flush()
		}
	}
	return err
}

func (w *antiStallProGinWriter) transportByteCount() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.transportBytes
}

// Size excludes transport-only keepalive events. A keepalive must not make
// openAIForwardMayFailover believe user-visible response data was committed.
func (w *antiStallProGinWriter) Size() int {
	if w == nil || w.ResponseWriter == nil {
		return -1
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	size := w.ResponseWriter.Size()
	if size < 0 {
		return size
	}
	if semantic := size - w.transportBytes; semantic > 0 {
		return semantic
	}
	return -1
}

func (w *antiStallProGinWriter) Write(p []byte) (int, error) {
	if w == nil || w.session == nil {
		return w.ResponseWriter.Write(p)
	}
	// Always report full accept to upstream copier.
	accepted := len(p)
	w.mu.Lock()
	defer w.mu.Unlock()

	// Buffer incomplete SSE lines.
	w.lineBuf = append(w.lineBuf, p...)
	for {
		idx := bytes.Index(w.lineBuf, []byte("\n\n"))
		if idx < 0 {
			break
		}
		block := append([]byte(nil), w.lineBuf[:idx+2]...)
		w.lineBuf = w.lineBuf[idx+2:]
		weight := EstimateSSETokenWeight(block)
		// Offer() also performs reconnect detection: if we were dripping,
		// new upstream data exits drip so client is not stuck at 1 tok/s.
		for _, flush := range w.session.Offer(block, weight) {
			if err := w.writeDownstream(flush); err != nil {
				return accepted, err
			}
		}
	}
	return accepted, nil
}

func (w *antiStallProGinWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *antiStallProGinWriter) Flush() {
	if fl, ok := w.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
}

func (w *antiStallProGinWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// RunAntiStallDripLoop emits held tokens at drip rate until:
//   - stop is closed
//   - client disconnects
//   - reconnect detected (dripMode cleared by Offer)
//   - drip timed out (MaxDripSeconds) — caller should switch leaf
//
// Important: when new leaf/upstream data arrives, Offer exits drip immediately
// so the client is never stuck forever at ~1 token/s.
func RunAntiStallDripLoop(c *gin.Context, session *AntiStallSession, stop <-chan struct{}) {
	runAntiStallDripLoop(c, session, stop, antiStallKeepaliveInterval)
}

func runAntiStallDripLoop(c *gin.Context, session *AntiStallSession, stop <-chan struct{}, keepaliveInterval time.Duration) {
	if c == nil || session == nil {
		return
	}
	if keepaliveInterval <= 0 {
		keepaliveInterval = antiStallKeepaliveInterval
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		if c.Request != nil {
			select {
			case <-c.Request.Context().Done():
				return
			default:
			}
		}

		// Reconnect or recovery ended: stop dripping immediately.
		if !session.IsDripping() {
			return
		}
		// Timeout: stop drip loop; gateway ShouldSwitchLeaf will force leaf switch.
		if session.DripTimedOut() {
			return
		}

		payload, wait, ok := session.TickDrip()
		if !ok {
			// The reserve may be empty before the first token or after a long
			// recovery. Keep the same SSE connection alive while another account
			// or leaf is selected. Codex parses this unknown event to reset its
			// stream idle timer, then discards it as non-semantic.
			wrapped, wrappedOK := c.Writer.(*antiStallProGinWriter)
			if wrappedOK && wrapped != nil {
				if err := wrapped.keepaliveWrite(); err != nil {
					return
				}
			}
			select {
			case <-stop:
				return
			case <-time.After(keepaliveInterval):
			}
			continue
		}
		// Prefer dripWrite on the hold-back wrapper (mutex vs Offer path).
		var writeErr error
		if wrapped, ok := c.Writer.(*antiStallProGinWriter); ok && wrapped != nil {
			writeErr = wrapped.dripWrite(payload)
		} else {
			_, writeErr = c.Writer.Write(payload)
			if writeErr == nil {
				if fl, ok := c.Writer.(http.Flusher); ok {
					fl.Flush()
				}
			}
		}
		if writeErr != nil {
			return
		}
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
	}
}

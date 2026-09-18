package service

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

// Anti-Stall PRO (抗中断) — hold-back token buffer + slow drip during upstream
// recovery, then Adaptive leaf switch when retries are exhausted and the buffer
// is nearly empty. Not competitive concurrency; sequential recovery only.
//
// Users pick a tier on the API key (basic/pro/ultra/off). Admins configure
// per-tier parameters. Reconnect detection exits drip as soon as new upstream
// data arrives so clients are never stuck at 1 token/s forever.

const (
	AntiStallProProductName = "Anti-Stall PRO"

	AntiStallTierOff   = "off"
	AntiStallTierBasic = "basic"
	AntiStallTierPro   = "pro"
	AntiStallTierUltra = "ultra"

	DefaultAntiStallBufferTokens      = 32
	DefaultAntiStallDripTokensPerSec  = 1
	DefaultAntiStallUpstreamMaxRetry  = 3
	DefaultAntiStallLowBufferTokens   = 4
	DefaultAntiStallMaxHoldBackTokens = 256
	DefaultAntiStallMaxDripSeconds    = 30
	DefaultAntiStallMaxLeafSwitches   = 3
)

// AntiStallTierParams is admin-tunable parameters for one tier.
type AntiStallTierParams struct {
	// BufferTokens: hold-back size during healthy streaming.
	BufferTokens int `json:"buffer_tokens"`
	// DripTokensPerSecond: client emit rate while recovering.
	DripTokensPerSecond int `json:"drip_tokens_per_second"`
	// UpstreamMaxRetry: same-leaf failures before leaf switch is allowed (when buffer low).
	UpstreamMaxRetry int `json:"upstream_max_retry"`
	// LowBufferTokens: reserve at or below this + retries exhausted → allow leaf switch.
	LowBufferTokens int `json:"low_buffer_tokens"`
	// MaxDripSeconds: max time spent in drip mode before force leaf switch / exit.
	// Prevents clients from being stuck at slow drip forever if reconnect fails.
	MaxDripSeconds int `json:"max_drip_seconds"`
	// MaxLeafSwitches: cap Adaptive leaf switches under anti-stall for one request.
	MaxLeafSwitches int `json:"max_leaf_switches"`
}

// AntiStallAdminConfig is stored as JSON under settings key anti_stall_pro.
// Global ModuleEnabled lets admins disable the feature for everyone.
// Per-tier params are used when a key selects basic/pro/ultra.
type AntiStallAdminConfig struct {
	ModuleEnabled bool                `json:"module_enabled"`
	Basic         AntiStallTierParams `json:"basic"`
	Pro           AntiStallTierParams `json:"pro"`
	Ultra         AntiStallTierParams `json:"ultra"`
}

// AntiStallProSettings is the resolved runtime config for one request
// (tier params + enabled flag). Kept for session/writer compatibility.
type AntiStallProSettings struct {
	Enabled             bool   `json:"enabled"`
	BufferTokens        int    `json:"buffer_tokens"`
	DripTokensPerSecond int    `json:"drip_tokens_per_second"`
	UpstreamMaxRetry    int    `json:"upstream_max_retry"`
	LowBufferTokens     int    `json:"low_buffer_tokens"`
	MaxDripSeconds      int    `json:"max_drip_seconds"`
	MaxLeafSwitches     int    `json:"max_leaf_switches"`
	Tier                string `json:"tier,omitempty"`
}

func DefaultAntiStallTierParams(tier string) AntiStallTierParams {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case AntiStallTierPro:
		return AntiStallTierParams{
			BufferTokens:        48,
			DripTokensPerSecond: 1,
			UpstreamMaxRetry:    2,
			LowBufferTokens:     8,
			MaxDripSeconds:      45,
			MaxLeafSwitches:     4,
		}
	case AntiStallTierUltra:
		return AntiStallTierParams{
			BufferTokens:        96,
			DripTokensPerSecond: 1,
			UpstreamMaxRetry:    2,
			LowBufferTokens:     8,
			MaxDripSeconds:      60,
			MaxLeafSwitches:     5,
		}
	default: // basic
		return AntiStallTierParams{
			BufferTokens:        DefaultAntiStallBufferTokens,
			DripTokensPerSecond: DefaultAntiStallDripTokensPerSec,
			UpstreamMaxRetry:    2,
			LowBufferTokens:     8,
			MaxDripSeconds:      DefaultAntiStallMaxDripSeconds,
			MaxLeafSwitches:     DefaultAntiStallMaxLeafSwitches,
		}
	}
}

func DefaultAntiStallAdminConfig() AntiStallAdminConfig {
	return AntiStallAdminConfig{
		ModuleEnabled: true,
		Basic:         DefaultAntiStallTierParams(AntiStallTierBasic),
		Pro:           DefaultAntiStallTierParams(AntiStallTierPro),
		Ultra:         DefaultAntiStallTierParams(AntiStallTierUltra),
	}
}

func (p AntiStallTierParams) Normalize() AntiStallTierParams {
	out := p
	if out.BufferTokens <= 0 {
		out.BufferTokens = DefaultAntiStallBufferTokens
	}
	if out.BufferTokens > DefaultAntiStallMaxHoldBackTokens {
		out.BufferTokens = DefaultAntiStallMaxHoldBackTokens
	}
	if out.DripTokensPerSecond <= 0 {
		out.DripTokensPerSecond = DefaultAntiStallDripTokensPerSec
	}
	if out.DripTokensPerSecond > 20 {
		out.DripTokensPerSecond = 20
	}
	if out.UpstreamMaxRetry <= 0 {
		out.UpstreamMaxRetry = DefaultAntiStallUpstreamMaxRetry
	}
	if out.UpstreamMaxRetry > 10 {
		out.UpstreamMaxRetry = 10
	}
	if out.LowBufferTokens < 0 {
		out.LowBufferTokens = 0
	}
	if out.LowBufferTokens > out.BufferTokens {
		out.LowBufferTokens = out.BufferTokens
	}
	if out.MaxDripSeconds <= 0 {
		out.MaxDripSeconds = DefaultAntiStallMaxDripSeconds
	}
	if out.MaxDripSeconds > 300 {
		out.MaxDripSeconds = 300
	}
	if out.MaxLeafSwitches <= 0 {
		out.MaxLeafSwitches = DefaultAntiStallMaxLeafSwitches
	}
	if out.MaxLeafSwitches > 10 {
		out.MaxLeafSwitches = 10
	}
	return out
}

func (c AntiStallAdminConfig) Normalize() AntiStallAdminConfig {
	return AntiStallAdminConfig{
		ModuleEnabled: c.ModuleEnabled,
		Basic:         c.Basic.Normalize(),
		Pro:           c.Pro.Normalize(),
		Ultra:         c.Ultra.Normalize(),
	}
}

// NormalizeAntiStallTier validates key tier; empty → off.
func NormalizeAntiStallTier(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case AntiStallTierBasic, AntiStallTierPro, AntiStallTierUltra:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return AntiStallTierOff
	}
}

// ResolveAntiStallForKey returns runtime settings for a key tier.
// Enabled only when module is on and tier is not off.
func ResolveAntiStallForKey(admin AntiStallAdminConfig, tier string) AntiStallProSettings {
	admin = admin.Normalize()
	t := NormalizeAntiStallTier(tier)
	if !admin.ModuleEnabled || t == AntiStallTierOff {
		return AntiStallProSettings{Enabled: false, Tier: AntiStallTierOff}
	}
	var p AntiStallTierParams
	switch t {
	case AntiStallTierPro:
		p = admin.Pro
	case AntiStallTierUltra:
		p = admin.Ultra
	default:
		p = admin.Basic
		t = AntiStallTierBasic
	}
	p = p.Normalize()
	return AntiStallProSettings{
		Enabled:             true,
		BufferTokens:        p.BufferTokens,
		DripTokensPerSecond: p.DripTokensPerSecond,
		UpstreamMaxRetry:    p.UpstreamMaxRetry,
		LowBufferTokens:     p.LowBufferTokens,
		MaxDripSeconds:      p.MaxDripSeconds,
		MaxLeafSwitches:     p.MaxLeafSwitches,
		Tier:                t,
	}
}

// AntiStallForceLeafFromUpstream is true when an upstream error should force
// Adaptive to abandon the current leaf instead of retrying the same group.
func AntiStallForceLeafFromUpstream(statusCode int, _ []byte) bool {
	return statusCode == 401 || statusCode == 402 || statusCode == 403 || statusCode == 429 || statusCode >= 500
}

// Legacy DefaultAntiStallProSettings kept for API compatibility (module defaults).
func DefaultAntiStallProSettings() AntiStallProSettings {
	return ResolveAntiStallForKey(DefaultAntiStallAdminConfig(), AntiStallTierOff)
}

func (s AntiStallProSettings) Normalize() AntiStallProSettings {
	// Map into tier params normalize via temporary admin-less path.
	p := AntiStallTierParams{
		BufferTokens:        s.BufferTokens,
		DripTokensPerSecond: s.DripTokensPerSecond,
		UpstreamMaxRetry:    s.UpstreamMaxRetry,
		LowBufferTokens:     s.LowBufferTokens,
		MaxDripSeconds:      s.MaxDripSeconds,
		MaxLeafSwitches:     s.MaxLeafSwitches,
	}.Normalize()
	s.BufferTokens = p.BufferTokens
	s.DripTokensPerSecond = p.DripTokensPerSecond
	s.UpstreamMaxRetry = p.UpstreamMaxRetry
	s.LowBufferTokens = p.LowBufferTokens
	s.MaxDripSeconds = p.MaxDripSeconds
	s.MaxLeafSwitches = p.MaxLeafSwitches
	return s
}

// AntiStallToken is one held-back stream fragment (usually a content delta).
type AntiStallToken struct {
	Payload []byte
	Weight  int
}

// AntiStallSession holds per-request reserve buffer and recovery state.
type AntiStallSession struct {
	mu sync.Mutex

	cfg AntiStallProSettings

	reserve       []AntiStallToken
	reserveWeight int

	dripMode      bool
	dripStartedAt time.Time
	upstreamFails int
	leafSwitches  int
	// reconnectSeen is set when Offer receives data after recovery started.
	reconnectSeen bool
}

func NewAntiStallSession(cfg AntiStallProSettings) *AntiStallSession {
	return &AntiStallSession{cfg: cfg.Normalize()}
}

func (s *AntiStallSession) Config() AntiStallProSettings {
	if s == nil {
		return DefaultAntiStallProSettings()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Offer accepts a newly received upstream fragment.
// Reconnect detection: if we were dripping and new data arrives, exit drip
// immediately and resume normal hold-back streaming.
func (s *AntiStallSession) Offer(payload []byte, weight int) (flushNow [][]byte) {
	if s == nil || len(payload) == 0 {
		return nil
	}
	if weight < 1 {
		weight = 1
	}
	cp := append([]byte(nil), payload...)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Reconnect / new-leaf data: leave drip mode so user is not stuck at 1 tok/s.
	if s.dripMode {
		s.dripMode = false
		s.dripStartedAt = time.Time{}
		s.upstreamFails = 0
		s.reconnectSeen = true
	}

	s.reserve = append(s.reserve, AntiStallToken{Payload: cp, Weight: weight})
	s.reserveWeight += weight

	// Healthy path: keep BufferTokens in reserve, flush the excess FIFO.
	for s.reserveWeight > s.cfg.BufferTokens && len(s.reserve) > 0 {
		tok := s.reserve[0]
		s.reserve = s.reserve[1:]
		s.reserveWeight -= tok.Weight
		if s.reserveWeight < 0 {
			s.reserveWeight = 0
		}
		flushNow = append(flushNow, tok.Payload)
	}
	return flushNow
}

// BeginRecovery marks upstream failure and enters drip mode.
func (s *AntiStallSession) BeginRecovery() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dripMode {
		s.dripStartedAt = time.Now()
	}
	s.dripMode = true
	s.reconnectSeen = false
	s.upstreamFails++
}

// IsDripping reports whether the session is currently in recovery drip mode.
func (s *AntiStallSession) IsDripping() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dripMode
}

// ReconnectSeen reports whether Offer detected new upstream data after recovery.
func (s *AntiStallSession) ReconnectSeen() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reconnectSeen
}

// EndRecovery leaves drip mode after a healthy upstream is reattached.
func (s *AntiStallSession) EndRecovery() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dripMode = false
	s.dripStartedAt = time.Time{}
	s.upstreamFails = 0
	s.reconnectSeen = true
}

// RecordLeafSwitch increments leaf switch counter and keeps drip until
// the new leaf produces data (Offer will clear drip on reconnect).
func (s *AntiStallSession) RecordLeafSwitch() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leafSwitches++
	s.upstreamFails = 0
	s.dripMode = true
	s.dripStartedAt = time.Now()
	s.reconnectSeen = false
}

// TickDrip returns at most one payload under drip mode.
// Returns ok=false when not dripping, empty, or drip timed out (caller should switch leaf).
func (s *AntiStallSession) TickDrip() (payload []byte, wait time.Duration, ok bool) {
	if s == nil {
		return nil, 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dripMode || len(s.reserve) == 0 {
		return nil, 0, false
	}
	// Drip timeout: stop slow drip; force switch path.
	if s.dripTimedOutLocked() {
		return nil, 0, false
	}
	tok := s.reserve[0]
	s.reserve = s.reserve[1:]
	s.reserveWeight -= tok.Weight
	if s.reserveWeight < 0 {
		s.reserveWeight = 0
	}
	perSec := s.cfg.DripTokensPerSecond
	if perSec < 1 {
		perSec = 1
	}
	wait = time.Second / time.Duration(perSec)
	if wait < 50*time.Millisecond {
		wait = 50 * time.Millisecond
	}
	return tok.Payload, wait, true
}

func (s *AntiStallSession) dripTimedOutLocked() bool {
	if !s.dripMode || s.dripStartedAt.IsZero() {
		return false
	}
	max := s.cfg.MaxDripSeconds
	if max <= 0 {
		max = DefaultAntiStallMaxDripSeconds
	}
	return time.Since(s.dripStartedAt) >= time.Duration(max)*time.Second
}

// DripTimedOut reports whether recovery drip exceeded MaxDripSeconds.
func (s *AntiStallSession) DripTimedOut() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dripTimedOutLocked()
}

// ReserveWeight is how much content is still held back.
func (s *AntiStallSession) ReserveWeight() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reserveWeight
}

// UpstreamFails returns consecutive upstream failures on the current leaf.
func (s *AntiStallSession) UpstreamFails() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upstreamFails
}

// LeafSwitches returns how many Adaptive leaf switches were done.
func (s *AntiStallSession) LeafSwitches() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leafSwitches
}

// ShouldSwitchLeaf reports whether Adaptive should move to the next leaf.
// Early switch: if next leaf is significantly healthier (score > 0.7) and current
// leaf has failed once+ with buffer remaining, switch immediately.
// Original logic: (retries exhausted AND buffer low) OR drip timed out.
// Always respects leaf switch budget.
func (s *AntiStallSession) ShouldSwitchLeaf(nextLeafHealthScore float64) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leafSwitches >= s.cfg.MaxLeafSwitches {
		return false // no more switches; let request fail cleanly
	}
	if s.dripTimedOutLocked() {
		return true
	}
	// Early switch: if next leaf is healthy (>0.7) and current leaf has failed
	// at least once, switch immediately even with buffer remaining.
	if nextLeafHealthScore > 0.7 && s.upstreamFails >= 1 && s.reserveWeight > s.cfg.LowBufferTokens {
		return true
	}
	// Original logic: wait for retries exhausted AND buffer low
	if s.upstreamFails < s.cfg.UpstreamMaxRetry {
		return false
	}
	return s.reserveWeight <= s.cfg.LowBufferTokens
}

// ShouldFailHard: drip timed out / retries exhausted and no more leaf switches.
func (s *AntiStallSession) ShouldFailHard() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leafSwitches < s.cfg.MaxLeafSwitches {
		return false
	}
	// No more leaves: fail immediately when retries exhausted or drip timed out.
	// Also fail if reserve is empty so client is not stuck at idle drip.
	if s.dripTimedOutLocked() || s.upstreamFails >= s.cfg.UpstreamMaxRetry {
		return true
	}
	return s.dripMode && s.reserveWeight <= 0 && s.upstreamFails > 0
}

// FlushAll drains remaining reserve (e.g. successful completion).
func (s *AntiStallSession) FlushAll() [][]byte {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, 0, len(s.reserve))
	for _, tok := range s.reserve {
		out = append(out, tok.Payload)
	}
	s.reserve = nil
	s.reserveWeight = 0
	s.dripMode = false
	s.dripStartedAt = time.Time{}
	return out
}

// EstimateSSETokenWeight approximates token weight from an SSE data block.
func EstimateSSETokenWeight(payload []byte) int {
	if len(payload) == 0 {
		return 1
	}
	lower := bytes.ToLower(payload)
	if !bytes.Contains(lower, []byte("content")) && !bytes.Contains(lower, []byte("\"delta\"")) {
		return 1
	}
	n := 0
	inWord := false
	for _, b := range payload {
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			if inWord {
				n++
				inWord = false
			}
			continue
		}
		inWord = true
	}
	if inWord {
		n++
	}
	if n < 1 {
		return 1
	}
	if n > 16 {
		return 16
	}
	return n
}

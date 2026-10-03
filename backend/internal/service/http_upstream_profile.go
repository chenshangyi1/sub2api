package service

import (
	"context"
	"time"
)

// HTTPUpstreamProfile marks HTTP upstream requests that need provider-specific
// transport policy.
type HTTPUpstreamProfile string

const (
	HTTPUpstreamProfileDefault      HTTPUpstreamProfile = ""
	HTTPUpstreamProfileOpenAI       HTTPUpstreamProfile = "openai"
	HTTPUpstreamProfileOpenAIImages HTTPUpstreamProfile = "openai_images"
	HTTPUpstreamProfileGrok         HTTPUpstreamProfile = "grok"
)

type httpUpstreamProfileContextKey struct{}
type httpUpstreamResponseHeaderTimeoutContextKey struct{}
type httpUpstreamDisableRedirectsContextKey struct{}
type httpUpstreamPublicHostsOnlyContextKey struct{}

// WithHTTPUpstreamProfile injects an upstream transport profile into ctx.
func WithHTTPUpstreamProfile(ctx context.Context, profile HTTPUpstreamProfile) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if profile == HTTPUpstreamProfileDefault {
		return ctx
	}
	return context.WithValue(ctx, httpUpstreamProfileContextKey{}, profile)
}

// HTTPUpstreamProfileFromContext resolves the upstream transport profile from ctx.
func HTTPUpstreamProfileFromContext(ctx context.Context) HTTPUpstreamProfile {
	if ctx == nil {
		return HTTPUpstreamProfileDefault
	}
	profile, ok := ctx.Value(httpUpstreamProfileContextKey{}).(HTTPUpstreamProfile)
	if !ok {
		return HTTPUpstreamProfileDefault
	}
	switch profile {
	case HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileOpenAIImages, HTTPUpstreamProfileGrok:
		return profile
	default:
		return HTTPUpstreamProfileDefault
	}
}

// WithHTTPUpstreamResponseHeaderTimeout overrides the transport-level
// ResponseHeaderTimeout for this upstream request. It only limits waiting for
// response headers, so successful streaming responses are not canceled mid-body.
func WithHTTPUpstreamResponseHeaderTimeout(ctx context.Context, timeout time.Duration) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return ctx
	}
	return context.WithValue(ctx, httpUpstreamResponseHeaderTimeoutContextKey{}, timeout)
}

// HTTPUpstreamResponseHeaderTimeoutFromContext returns a per-request response
// header timeout override when one was explicitly set.
func HTTPUpstreamResponseHeaderTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	if ctx == nil {
		return 0, false
	}
	timeout, ok := ctx.Value(httpUpstreamResponseHeaderTimeoutContextKey{}).(time.Duration)
	if !ok || timeout <= 0 {
		return 0, false
	}
	return timeout, true
}

// WithHTTPUpstreamRedirectsDisabled prevents credential-bearing probes from
// following redirects through the shared upstream client.
func WithHTTPUpstreamRedirectsDisabled(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, httpUpstreamDisableRedirectsContextKey{}, true)
}

func HTTPUpstreamRedirectsDisabled(ctx context.Context) bool {
	return ctx != nil && ctx.Value(httpUpstreamDisableRedirectsContextKey{}) == true
}

// WithHTTPUpstreamPublicHostsOnly marks a request whose destination, and every
// redirect hop after it, must resolve to a public address. The shared upstream
// client enforces it regardless of the security.url_allowlist configuration;
// use it for fetches whose URL comes from an untrusted upstream response.
func WithHTTPUpstreamPublicHostsOnly(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, httpUpstreamPublicHostsOnlyContextKey{}, true)
}

func HTTPUpstreamPublicHostsOnly(ctx context.Context) bool {
	return ctx != nil && ctx.Value(httpUpstreamPublicHostsOnlyContextKey{}) == true
}

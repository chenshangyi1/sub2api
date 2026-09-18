package service

import "testing"

func TestJoinMonitorURL_DoesNotDuplicateFullAPIPath(t *testing.T) {
	t.Parallel()
	if got := joinURL("https://x.example/v1/messages", "/v1/messages"); got != "https://x.example/v1/messages" {
		t.Fatalf("joinURL duplicated messages path: %q", got)
	}
	if got := joinURL("https://x.example/anthropic/v1/messages", "/v1/messages"); got != "https://x.example/anthropic/v1/messages" {
		t.Fatalf("joinURL duplicated anthropic messages path: %q", got)
	}
}

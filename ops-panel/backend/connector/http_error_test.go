package connector

import (
	"fmt"
	"testing"
)

func TestHTTPStatusCodeExtractsWrappedStatus(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", HTTPStatusError(404, []byte(`{"error":"missing"}`)))
	if got, ok := HTTPStatusCode(err); !ok || got != 404 {
		t.Fatalf("HTTPStatusCode() = %d, %v; want 404, true", got, ok)
	}
}

func TestHTTPStatusErrorSummaryIsPreserved(t *testing.T) {
	err := HTTPStatusError(503, []byte(`{"message":"upstream busy"}`))
	if got := err.Error(); got != "status 503: upstream busy" {
		t.Fatalf("error = %q", got)
	}
}

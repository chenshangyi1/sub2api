package connector

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const maxHTTPErrorSummaryRunes = 300

// HTTPStatusError preserves the response code so callers can distinguish an
// optional endpoint that is not implemented from an upstream outage.
type StatusError struct {
	Code    int
	Summary string
}

func (e *StatusError) Error() string {
	if e.Summary == "" {
		return fmt.Sprintf("status %d", e.Code)
	}
	return fmt.Sprintf("status %d: %s", e.Code, e.Summary)
}

func HTTPStatusError(statusCode int, body []byte) error {
	summary := summarizeHTTPErrorBody(body)
	return &StatusError{Code: statusCode, Summary: summary}
}

func HTTPStatusCode(err error) (int, bool) {
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr == nil {
		return 0, false
	}
	return statusErr.Code, true
}

func summarizeHTTPErrorBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		for _, key := range []string{"message", "error", "detail", "msg"} {
			if value, ok := payload[key]; ok {
				msg := strings.TrimSpace(fmt.Sprint(value))
				if msg != "" {
					return truncateHTTPErrorSummary(collapseHTTPErrorWhitespace(msg))
				}
			}
		}
	}

	if title := htmlTagText(text, "title"); title != "" {
		return truncateHTTPErrorSummary(collapseHTTPErrorWhitespace(title))
	}
	if h1 := htmlTagText(text, "h1"); h1 != "" {
		return truncateHTTPErrorSummary(collapseHTTPErrorWhitespace(h1))
	}

	return truncateHTTPErrorSummary(collapseHTTPErrorWhitespace(text))
}

func htmlTagText(text string, tag string) string {
	lower := strings.ToLower(text)
	open := "<" + tag
	start := strings.Index(lower, open)
	if start < 0 {
		return ""
	}
	gt := strings.Index(lower[start:], ">")
	if gt < 0 {
		return ""
	}
	contentStart := start + gt + 1
	closeTag := "</" + tag + ">"
	end := strings.Index(lower[contentStart:], closeTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(text[contentStart : contentStart+end])
}

func collapseHTTPErrorWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateHTTPErrorSummary(s string) string {
	runes := []rune(s)
	if len(runes) <= maxHTTPErrorSummaryRunes {
		return s
	}
	return string(runes[:maxHTTPErrorSummaryRunes]) + "..."
}

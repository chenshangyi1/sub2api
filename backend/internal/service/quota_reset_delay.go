package service

import (
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const quotaResetDelayCap = time.Hour

var (
	quotaResetAfterPattern = regexp.MustCompile(`(?i)(?:reset(?:s)?(?:\s+in|\s+after)|retry(?:\s+after|\s+in)|try again in)\s+([0-9]+(?:\.[0-9]+)?\s*[a-z]+(?:\s*[0-9]+(?:\.[0-9]+)?\s*[a-z]+)*)`)
	quotaResetTokenPattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(ms|milliseconds?|s|secs?|seconds?|m|mins?|minutes?|h|hrs?|hours?|d|days?)`)
)

func parseQuotaResetDelay(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 {
		return capQuotaResetDelay(time.Duration(seconds * float64(time.Second)))
	}
	if parsed, err := time.ParseDuration(strings.ReplaceAll(raw, " ", "")); err == nil && parsed > 0 {
		return capQuotaResetDelay(parsed)
	}
	match := quotaResetAfterPattern.FindStringSubmatch(raw)
	if len(match) == 2 {
		if delay := parseCompactDurationTokens(match[1]); delay > 0 {
			return capQuotaResetDelay(delay)
		}
	}
	if delay := parseCompactDurationTokens(raw); delay > 0 {
		return capQuotaResetDelay(delay)
	}
	return 0
}

func parseCompactDurationTokens(raw string) time.Duration {
	matches := quotaResetTokenPattern.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return 0
	}
	var total time.Duration
	for _, match := range matches {
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil || value <= 0 {
			continue
		}
		switch strings.ToLower(match[2]) {
		case "ms", "millisecond", "milliseconds":
			total += time.Duration(value * float64(time.Millisecond))
		case "s", "sec", "secs", "second", "seconds":
			total += time.Duration(value * float64(time.Second))
		case "m", "min", "mins", "minute", "minutes":
			total += time.Duration(value * float64(time.Minute))
		case "h", "hr", "hrs", "hour", "hours":
			total += time.Duration(value * float64(time.Hour))
		case "d", "day", "days":
			total += time.Duration(value * float64(24*time.Hour))
		}
	}
	return total
}

func capQuotaResetDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return 0
	}
	if delay > quotaResetDelayCap {
		return quotaResetDelayCap
	}
	if delay < time.Second {
		return time.Second
	}
	return delay
}

func parseRetryAfterResetTime(headers http.Header, now time.Time) *time.Time {
	if headers == nil {
		return nil
	}
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		resetAt := now.Add(time.Duration(seconds * float64(time.Second)))
		return &resetAt
	}
	if parsed, err := http.ParseTime(raw); err == nil {
		return &parsed
	}
	if delay := parseQuotaResetDelay(raw); delay > 0 {
		resetAt := now.Add(delay)
		return &resetAt
	}
	return nil
}

func unixTimestampAfterDelay(delay time.Duration) *int64 {
	if delay <= 0 {
		return nil
	}
	ts := time.Now().Unix() + int64(math.Ceil(delay.Seconds()))
	return &ts
}

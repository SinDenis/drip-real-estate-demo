package eiszhs

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CurrentRequestError classifies an unsuccessful current endpoint attempt for durable retry planning.
// DEN-54 records the policy signal but leaves multi-attempt backoff orchestration to DEN-59.
type CurrentRequestError struct {
	StatusCode int
	Retryable  bool
	RetryAfter time.Duration
	Cause      error
}

func (requestErr *CurrentRequestError) Error() string {
	if requestErr.StatusCode == 0 {
		return fmt.Sprintf("ЕИСЖС current request failed: %v", requestErr.Cause)
	}
	return fmt.Sprintf("ЕИСЖС current data returned HTTP %d", requestErr.StatusCode)
}

func (requestErr *CurrentRequestError) Unwrap() error {
	return requestErr.Cause
}

func classifyCurrentStatus(statusCode int, retryAfterHeader string, now time.Time) *CurrentRequestError {
	return &CurrentRequestError{
		StatusCode: statusCode,
		Retryable: statusCode == http.StatusTooManyRequests || statusCode == http.StatusInternalServerError ||
			statusCode == http.StatusServiceUnavailable,
		RetryAfter: parseRetryAfter(retryAfterHeader, now),
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

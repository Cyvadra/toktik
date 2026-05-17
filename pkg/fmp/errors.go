package fmp

import (
	"errors"
	"fmt"
	"time"
)

// HTTPStatusError captures non-200 HTTP responses from FMP.
type HTTPStatusError struct {
	URL        string
	StatusCode int
	Status     string
	Body       string
	RetryAfter time.Duration
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "fmp: http error"
	}
	if e.Body == "" {
		return fmt.Sprintf("fmp request %s failed: %s", e.URL, e.Status)
	}
	return fmt.Sprintf("fmp request %s failed: %s body=%s", e.URL, e.Status, e.Body)
}

// IsHTTPStatus reports whether err (possibly wrapped) is an HTTPStatusError
// with the provided status code.
func IsHTTPStatus(err error, statusCode int) bool {
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == statusCode
	}
	return false
}

func RetryAfterDelay(err error) time.Duration {
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.RetryAfter
	}
	return 0
}

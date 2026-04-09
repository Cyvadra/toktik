package polygon

import (
	"errors"
	"fmt"
)

type HTTPStatusError struct {
	URL        string
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Body == "" {
		return fmt.Sprintf("polygon request %s failed: %s", e.URL, e.Status)
	}
	return fmt.Sprintf("polygon request %s failed: %s body=%s", e.URL, e.Status, e.Body)
}

func (e *HTTPStatusError) IsStatus(code int) bool {
	return e != nil && e.StatusCode == code
}

func IsHTTPStatus(err error, code int) bool {
	var statusErr *HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.IsStatus(code)
}

package polygon

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type apiResponse interface {
	StatusCode() int
	Status() string
}

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

func normalizeResponseError(resp apiResponse, httpResp *http.Response, body []byte, err error) error {
	if err == nil {
		return nil
	}
	if resp == nil || resp.StatusCode() < http.StatusBadRequest {
		return err
	}
	url := ""
	if httpResp != nil && httpResp.Request != nil && httpResp.Request.URL != nil {
		url = httpResp.Request.URL.String()
	}
	return &HTTPStatusError{
		URL:        url,
		StatusCode: resp.StatusCode(),
		Status:     strings.TrimSpace(resp.Status()),
		Body:       strings.TrimSpace(string(body)),
	}
}

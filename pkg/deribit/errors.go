package deribit

import "fmt"

// RequestError represents a failure to reach or read from Deribit.
type RequestError struct {
	Err error
}

func (e *RequestError) Error() string {
	if e == nil || e.Err == nil {
		return "deribit request failed"
	}
	return "deribit request failed: " + e.Err.Error()
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// HTTPStatusError represents a non-successful response from Deribit.
type HTTPStatusError struct {
	StatusCode int
	Status     string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("deribit request failed: %s", e.Status)
}

// RPCError represents an error returned in a successful Deribit JSON-RPC response.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("deribit error code=%d message=%s", e.Code, e.Message)
}

// ResponseError indicates that Deribit returned an unusable response payload.
type ResponseError struct {
	Message string
}

func (e *ResponseError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return "invalid deribit response: " + e.Message
}

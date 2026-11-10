package buildkite

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type RequestTimeout struct {
	Method            string
	Path              string
	MutationAttempted bool
	Err               error
}

func (e *RequestTimeout) Error() string { return e.Err.Error() }
func (e *RequestTimeout) Unwrap() error { return e.Err }

type APIError struct {
	StatusCode int
	Message    string `json:"message"`
	Code       string `json:"code"`
	// Body is the raw response, for endpoints whose errors carry structured details.
	Body []byte `json:"-"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("Buildkite API returned %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("Buildkite API returned %d: %s", e.StatusCode, e.Message)
}

func decodeAPIError(response *http.Response) error {
	apiError := &APIError{StatusCode: response.StatusCode, Message: response.Status}
	apiError.Body, _ = io.ReadAll(response.Body)
	_ = json.Unmarshal(apiError.Body, apiError)
	return apiError
}

package buildkite

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type APIError struct {
	StatusCode int
	Message    string `json:"message"`
	Code       string `json:"code"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("Buildkite API returned %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("Buildkite API returned %d: %s", e.StatusCode, e.Message)
}

func decodeAPIError(response *http.Response) error {
	apiError := &APIError{StatusCode: response.StatusCode, Message: response.Status}
	_ = json.NewDecoder(response.Body).Decode(apiError)
	return apiError
}

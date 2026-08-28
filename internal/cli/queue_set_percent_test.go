package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueueSetPercentValidatesRange(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	err := Run(context.Background(), []string{
		"--organization", "acme",
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "101",
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, server.Client())
	if err == nil || !strings.Contains(err.Error(), "between 0 and 100") {
		t.Fatalf("error = %v", err)
	}
}

package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueueConfigureDryRunDoesNotMutate(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case "/v2/organizations/acme/clusters/cluster-id/queues":
			_, _ = w.Write([]byte(`[{"id":"queue-id","key":"test"}]`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--organization", "acme",
		"--api-url", server.URL,
		"--dry-run",
		"queue", "configure", "test",
		"--destination-cluster", "production",
	}, strings.NewReader(""), &stdout, &bytes.Buffer{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if !strings.Contains(stdout.String(), "test") || !strings.Contains(stdout.String(), "0%") {
		t.Fatalf("output = %q", stdout.String())
	}
}

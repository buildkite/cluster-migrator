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
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "101",
	}, &bytes.Buffer{}, &bytes.Buffer{}, organizationClient(server.Client()))
	if err == nil || !strings.Contains(err.Error(), "between 0 and 100") {
		t.Fatalf("error = %v", err)
	}
}

func TestQueueSetPercentPrintsChange(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":25}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "25",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE SET-PERCENT\nQueue: test\nDestination: production\n\nRESULT\n\nRouting: 10% → 25%\n\nNEXT STEPS\n\n1. Review destination activity:\n\n   cluster-migrator queue metrics test\n\n2. When ready, increase routing:\n\n   cluster-migrator queue set-percent test --to <percentage>\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueSetPercentToZeroPrintsBeginRoutingGuidance(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":25}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id"},"routed_percent":0}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "set-percent", "test", "--to", "0",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE SET-PERCENT\nQueue: test\nDestination: production\n\nRESULT\n\nRouting: 25% → 0%\n\nNEXT STEPS\n\n1. Scale the destination infrastructure.\n2. Once applied, begin routing:\n\n   cluster-migrator queue set-percent test --to <percentage>\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

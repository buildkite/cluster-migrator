package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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
		"--endpoint", server.URL,
		"--dry-run",
		"queue", "configure", "test",
		"--destination-cluster", "production",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	want := "QUEUE CONFIGURE\nQueue: test\nDestination: production\n\nRESULT (dry run)\n\nRouting: — → 0%\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueConfigurePrintsNextStep(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters/cluster-id/queues":
			_, _ = w.Write([]byte(`[{"id":"queue-id","key":"test"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"test"},"routed_percent":0}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "configure", "test",
		"--destination-cluster", "production",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE CONFIGURE\nQueue: test\nDestination: production\n\nRESULT\n\nRouting: — → 0%\n\nNEXT\n\nScale the destination infrastructure. When ready, begin routing:\n\n  cluster-migrator queue set-percent test --to <percentage>\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueueStatusPrintsTable(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"Cluster Migrator Demo"}]`))
		case "/v2/organizations/acme/cluster-queue-migrations/default":
			_, _ = w.Write([]byte(`{"queue_key":"default","destination":{"cluster_id":"cluster-id"},"routed_percent":25}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--organization", "acme",
		"--endpoint", server.URL,
		"queue", "status", "default",
	}, &stdout, &bytes.Buffer{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE    ROUTING  DESTINATION\ndefault  25%      Cluster Migrator Demo\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

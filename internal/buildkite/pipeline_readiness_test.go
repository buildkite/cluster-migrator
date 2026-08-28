package buildkite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetPipelineReadiness(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("destination_cluster_id"); got != "cluster-id" {
			t.Fatalf("destination_cluster_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pipeline":"monorepo","ready":true,"blocking_queues":[]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	readiness, err := client.GetPipelineReadiness(context.Background(), "monorepo", "cluster-id")
	if err != nil {
		t.Fatal(err)
	}
	if !readiness.Ready {
		t.Fatal("pipeline should be ready")
	}
}

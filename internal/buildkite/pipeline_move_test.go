package buildkite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMovePipeline(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/move" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","slug":"monorepo","cluster_id":"cluster-id"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	pipeline, err := client.MovePipeline(context.Background(), "monorepo", "cluster-id")
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.ID != "849411f9-9e6d-4739-a0d8-e247088e9b52" || pipeline.ClusterID != "cluster-id" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

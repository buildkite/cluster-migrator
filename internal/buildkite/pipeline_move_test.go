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
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %q, want PATCH", r.Method)
		}
		if r.URL.Path != "/v2/organizations/acme/pipelines/monorepo" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"slug":"monorepo","cluster":{"id":"cluster-id","name":"Production"}}`))
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
	if pipeline.Cluster == nil || pipeline.Cluster.ID != "cluster-id" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

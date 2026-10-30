package buildkite

import (
	"context"
	"io"
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
		if r.URL.Path != "/v2/organizations/acme/unclustered-to-clustered/migration/pipelines/monorepo" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 || r.URL.RawQuery != "" {
			t.Fatalf("unexpected move inputs: body = %q, query = %q, error = %v", body, r.URL.RawQuery, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","slug":"monorepo","cluster_id":"cluster-id"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	pipeline, err := client.MovePipeline(context.Background(), "monorepo")
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.ClusterID != "cluster-id" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

func TestMovePipelineRejectsMissingDestination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"slug":"monorepo"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.MovePipeline(context.Background(), "monorepo"); err == nil || err.Error() != "move pipeline response missing cluster_id" {
		t.Fatalf("error = %v, want missing cluster_id", err)
	}
}

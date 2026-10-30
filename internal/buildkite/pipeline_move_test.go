package buildkite

import (
	"context"
	"encoding/json"
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
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["cluster_id"] != "cluster-id" || len(body) != 1 {
			t.Fatalf("body = %#v, want only cluster_id", body)
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
	if pipeline.ClusterID != "cluster-id" {
		t.Fatalf("pipeline = %#v", pipeline)
	}
}

func TestMovePipelineRejectsUnexpectedDestination(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantError string
	}{
		{
			name:      "missing cluster ID",
			response:  `{"slug":"monorepo"}`,
			wantError: `move pipeline response cluster_id "" does not match destination "cluster-id"`,
		},
		{
			name:      "mismatched cluster ID",
			response:  `{"slug":"monorepo","cluster_id":"other-cluster"}`,
			wantError: `move pipeline response cluster_id "other-cluster" does not match destination "cluster-id"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.MovePipeline(context.Background(), "monorepo", "cluster-id"); err == nil || err.Error() != test.wantError {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
		})
	}
}

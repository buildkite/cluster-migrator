package buildkite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartConcurrencyGroupCutover(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups/deploy%2Fproduction/cutover" &&
			r.URL.RawPath != "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups/deploy%2Fproduction/cutover" {
			t.Fatalf("path = %q, raw path = %q", r.URL.Path, r.URL.RawPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"key":"deploy/production","state":"draining","destination_cluster_id":"cluster-id"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	group, err := client.StartConcurrencyGroupCutover(context.Background(), "deploy/production", "cluster-id")
	if err != nil {
		t.Fatal(err)
	}
	if group.State != "draining" {
		t.Fatalf("state = %q", group.State)
	}
}

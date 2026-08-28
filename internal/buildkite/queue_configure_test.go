package buildkite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigureQueueMigration(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		var body struct {
			ClusterID string `json:"cluster_id"`
			QueueKey  string `json:"queue_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.ClusterID != "cluster-id" {
			t.Fatalf("cluster ID = %q", body.ClusterID)
		}
		if body.QueueKey != "linux.cpu" {
			t.Fatalf("queue key = %q", body.QueueKey)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"queue_key":"linux.cpu","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"linux.cpu"},"routed_percent":0}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	migration, err := client.ConfigureQueueMigration(context.Background(), "linux.cpu", "cluster-id")
	if err != nil {
		t.Fatal(err)
	}
	if migration.QueueKey != "linux.cpu" || migration.RoutedPercent != 0 {
		t.Fatalf("migration = %#v", migration)
	}
}

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
		if r.URL.Path != "/v2/organizations/acme/unclustered-to-clustered/queue-migrations" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["queue_key"] != "linux.cpu" || len(body) != 1 {
			t.Fatalf("body = %#v, want only queue_key", body)
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

	migration, err := client.ConfigureQueueMigration(context.Background(), "linux.cpu")
	if err != nil {
		t.Fatal(err)
	}
	if migration.QueueKey != "linux.cpu" || migration.RoutedPercent != 0 {
		t.Fatalf("migration = %#v", migration)
	}
}

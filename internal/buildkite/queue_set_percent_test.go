package buildkite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetQueueMigrationPercent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("method = %q, want PATCH", r.Method)
		}
		if r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/linux" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		var body struct {
			RoutedPercent int `json:"routed_percent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.RoutedPercent != 30 {
			t.Fatalf("routed percent = %d", body.RoutedPercent)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"queue_key":"linux","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"linux"},"routed_percent":30}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	migration, err := client.SetQueueMigrationPercent(context.Background(), "linux", 30)
	if err != nil {
		t.Fatal(err)
	}
	if migration.RoutedPercent != 30 {
		t.Fatalf("routed percent = %d", migration.RoutedPercent)
	}
}

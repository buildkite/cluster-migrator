package buildkite

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListQueueMigrationsFollowsNextLinks(t *testing.T) {
	t.Parallel()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after") == "first" {
			_, _ = w.Write([]byte(`{"items":[{"queue_key":"second"}],"links":{}}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"items":[{"queue_key":"first"}],"links":{"next":%q}}`, server.URL+"/v2/organizations/acme/cluster-queue-migrations?after=first")
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	migrations, err := client.ListQueueMigrations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 || migrations[0].QueueKey != "first" || migrations[1].QueueKey != "second" {
		t.Fatalf("migrations = %#v", migrations)
	}
}

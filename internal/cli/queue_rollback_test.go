package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueueRollbackSetsZero(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"test"},"routed_percent":30}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/test":
			var body struct {
				RoutedPercent int `json:"routed_percent"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.RoutedPercent != 0 {
				t.Fatalf("routed percent = %d", body.RoutedPercent)
			}
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"test"},"routed_percent":0}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "rollback", "test",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE ROLLBACK\nQueue: test\nDestination: production\n\nRESULT\n\nRouting: 30% → 0%\n\nSOURCE\n\nNew jobs will return to the unclustered queue. Existing jobs remain where they were routed.\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQueueRollbackSetsZero(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"test"},"routed_percent":30}`))
			return
		}
		var body struct {
			RoutedPercent int `json:"routed_percent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.RoutedPercent != 0 {
			t.Fatalf("routed percent = %d", body.RoutedPercent)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"queue_key":"test","destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"test"},"routed_percent":0}`))
	}))
	defer server.Close()

	err := Run(context.Background(), []string{
		"--organization", "acme",
		"--endpoint", server.URL,
		"--yes",
		"queue", "rollback", "test",
	}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
}

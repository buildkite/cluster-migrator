package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestPipelineMoveWaitReportsProgressOnStderr(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/move":
			_, _ = w.Write([]byte(`{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","slug":"monorepo","cluster_id":"old-cluster-id"}`))
		case "/v2/organizations/acme/pipelines/monorepo":
			_, _ = w.Write([]byte(`{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","slug":"monorepo","name":"Monorepo","cluster_id":"cluster-id"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	app := Context{
		Context:     context.Background(),
		Client:      client,
		Output:      &stdout,
		ErrorOutput: &stderr,
		JSON:        true,
	}

	err = (&PipelineMoveCmd{Pipeline: "monorepo", DestinationCluster: "production", Wait: true}).Run(&app)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stderr.String(), "Waiting for pipeline monorepo to report cluster production…\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	want := "{\n  \"slug\": \"monorepo\",\n  \"name\": \"Monorepo\",\n  \"cluster_id\": \"cluster-id\"\n}\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

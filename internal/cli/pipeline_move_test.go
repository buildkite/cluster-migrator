package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestPipelineMovePrintsResult(t *testing.T) {
	app, stdout, stderr, requests := pipelineMoveTestContext(t, false, `{"slug":"demo-pipeline","name":"Demo Pipeline","cluster_id":"cluster-id"}`)

	err := (&PipelineMoveCmd{Pipeline: "demo-pipeline", DestinationCluster: "Production"}).Run(app)
	if err != nil {
		t.Fatal(err)
	}

	want := "PIPELINE MOVE\nPipeline: Demo Pipeline (demo-pipeline)\nDestination: Production\n\nRESULT\n\nPipeline moved to the Production cluster.\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	if got := strings.Join(*requests, "\n"); got != "GET /v2/organizations/acme/clusters?per_page=100\nPOST /v2/organizations/acme/cluster-queue-migrations/pipelines/demo-pipeline/move" {
		t.Fatalf("requests = %q", got)
	}
}

func TestPipelineMoveJSONPreservesAPIResponse(t *testing.T) {
	app, stdout, stderr, requests := pipelineMoveTestContext(t, true, `{"slug":"demo-pipeline","name":"Demo Pipeline","cluster_id":"cluster-id"}`)

	err := (&PipelineMoveCmd{Pipeline: "demo-pipeline", DestinationCluster: "Production"}).Run(app)
	if err != nil {
		t.Fatal(err)
	}

	want := "{\n  \"slug\": \"demo-pipeline\",\n  \"name\": \"Demo Pipeline\",\n  \"cluster_id\": \"cluster-id\"\n}\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	if got := strings.Join(*requests, "\n"); got != "GET /v2/organizations/acme/clusters?per_page=100\nPOST /v2/organizations/acme/cluster-queue-migrations/pipelines/demo-pipeline/move" {
		t.Fatalf("requests = %q", got)
	}
}

func TestPipelineMoveFallsBackToSlugWhenNameMissing(t *testing.T) {
	app, stdout, _, _ := pipelineMoveTestContext(t, false, `{"slug":"demo-pipeline","cluster_id":"cluster-id"}`)

	err := (&PipelineMoveCmd{Pipeline: "demo-pipeline", DestinationCluster: "Production"}).Run(app)
	if err != nil {
		t.Fatal(err)
	}
	want := "PIPELINE MOVE\nPipeline: demo-pipeline\nDestination: Production\n\nRESULT\n\nPipeline moved to the Production cluster.\n"
	if got := stdout.String(); got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func pipelineMoveTestContext(t *testing.T, jsonOutput bool, moveResponse string) (*Context, *bytes.Buffer, *bytes.Buffer, *[]string) {
	t.Helper()
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"Production"}]`)
		case "/v2/organizations/acme/cluster-queue-migrations/pipelines/demo-pipeline/move":
			_, _ = fmt.Fprint(w, moveResponse)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	app := &Context{
		Context:     context.Background(),
		Client:      client,
		Output:      &stdout,
		ErrorOutput: &stderr,
		JSON:        jsonOutput,
	}
	return app, &stdout, &stderr, &requests
}

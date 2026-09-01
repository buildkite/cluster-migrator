package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestPipelineReadinessRetriesPendingObservationBeforePrinting(t *testing.T) {
	readinessRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness":
			readinessRequests++
			if readinessRequests < 3 {
				_, _ = w.Write([]byte(`{"status":"observation_pending","retry_after_seconds":10}`))
				return
			}
			_, _ = w.Write([]byte(`{"pipeline":"monorepo","destination_cluster_id":"cluster-id","status":"no_known_blockers","queue_observation":{"observed_at":"2026-09-01T07:00:00Z","window_started_at":"2026-09-01T06:50:00Z","window_seconds":600,"complete":false,"blocking_queues":[]},"concurrency_group_observation":{"observed_at":"2026-09-01T07:00:00Z","window_started_at":"2026-09-01T06:50:00Z","window_seconds":600,"complete":false,"blocking_concurrency_groups":[]}}`))
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
	waits := 0
	app := Context{
		Context:     context.Background(),
		Client:      client,
		Output:      &stdout,
		ErrorOutput: &stderr,
		Wait: func(ctx context.Context, duration time.Duration) error {
			waits++
			if duration != 10*time.Second {
				t.Fatalf("wait duration = %s", duration)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout before final response = %q", stdout.String())
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("readiness retry context has no deadline")
			}
			return nil
		},
	}

	err = (&PipelineReadinessCmd{Pipeline: "monorepo", DestinationCluster: "production"}).Run(&app)
	if err != nil {
		t.Fatal(err)
	}
	if waits != 2 || readinessRequests != 3 {
		t.Fatalf("waits/requests = %d/%d", waits, readinessRequests)
	}
	if got, want := stderr.String(), "Pipeline assessment is still being prepared; retrying every 10 seconds…\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), `"status": "no_known_blockers"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPipelineReadinessPrintsBlockedAssessmentThenFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v2/organizations/acme/clusters" {
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
			return
		}
		_, _ = w.Write([]byte(`{"pipeline":"monorepo","destination_cluster_id":"cluster-id","status":"blocked","queue_observation":{"blocking_queues":[{"queue":"deploy","reasons":["active_source_jobs"],"routed_percent":100,"active_source_jobs":1}]},"concurrency_group_observation":{"blocking_concurrency_groups":[]}}`))
	}))
	defer server.Close()

	client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := Context{Context: context.Background(), Client: client, Output: &stdout, ErrorOutput: &bytes.Buffer{}}

	err = (&PipelineReadinessCmd{Pipeline: "monorepo", DestinationCluster: "production"}).Run(&app)
	if err == nil || err.Error() != "pipeline has known blockers" {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stdout.String(), `"status": "blocked"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestPipelineReadinessPreservesParentDeadline(t *testing.T) {
	client, err := buildkite.NewClient("https://example.com", "acme", "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	app := Context{Context: ctx, Client: client, Output: &bytes.Buffer{}, ErrorOutput: &bytes.Buffer{}}

	err = (&PipelineReadinessCmd{Pipeline: "monorepo", DestinationCluster: "production"}).Run(&app)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want parent deadline exceeded", err)
	}
}

func TestPipelineMoveDryRunRequiresNoKnownBlockers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness":
			_, _ = w.Write([]byte(`{"status":"no_known_blockers","queue_observation":{"blocking_queues":[]},"concurrency_group_observation":{"blocking_concurrency_groups":[]}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	app := Context{Context: context.Background(), Client: client, Output: &stdout, ErrorOutput: &bytes.Buffer{}, DryRun: true}

	if err := (&PipelineMoveCmd{Pipeline: "monorepo", DestinationCluster: "production"}).Run(&app); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "move pipeline monorepo") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestPipelineReadinessRejectsInvalidRetryInterval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v2/organizations/acme/clusters" {
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"observation_pending","retry_after_seconds":0}`)
	}))
	defer server.Close()

	client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	app := Context{Context: context.Background(), Client: client, Output: &bytes.Buffer{}, ErrorOutput: &bytes.Buffer{}}

	err = (&PipelineReadinessCmd{Pipeline: "monorepo", DestinationCluster: "production"}).Run(&app)
	if err == nil || err.Error() != "invalid retry_after_seconds: 0" {
		t.Fatalf("error = %v", err)
	}
}

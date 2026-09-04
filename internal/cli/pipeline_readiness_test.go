package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestPipelineReadinessPrintsNoKnownBlockersWithoutClaimingReadiness(t *testing.T) {
	queueNextRefreshAt := "2026-09-01T07:01:00Z"
	concurrencyGroupNextRefreshAt := "2026-09-01T07:01:30Z"
	readiness := &buildkite.PipelineReadiness{
		Pipeline:             "monorepo",
		DestinationClusterID: "cluster-id",
		Status:               buildkite.PipelineReadinessNoKnownBlockers,
		QueueObservation: buildkite.PipelineQueueObservation{
			NextRefreshAt: &queueNextRefreshAt,
			WindowSeconds: 600,
			Complete:      false,
		},
		ConcurrencyGroupObservation: buildkite.PipelineConcurrencyGroupObservation{
			NextRefreshAt: &concurrencyGroupNextRefreshAt,
			Complete:      true,
		},
	}

	var output bytes.Buffer
	app := Context{Output: &output, Now: func() time.Time { return time.Date(2026, time.September, 1, 7, 0, 48, 0, time.UTC) }}
	if err := app.Print(readiness); err != nil {
		t.Fatal(err)
	}

	want := "PIPELINE READINESS\nPipeline: monorepo\nDestination cluster: cluster-id\nStatus: NO KNOWN BLOCKERS — not proof of readiness\n\nQUEUE BLOCKERS\nNone\n\nCONCURRENCY-GROUP BLOCKERS\nNone\n\nOBSERVATIONS\nQueues: incomplete; 10-minute window; observation time unavailable\nQueue refresh eligible: in 12 seconds\nConcurrency groups: complete; window unavailable; observation time unavailable\nConcurrency-group refresh eligible: in 42 seconds\n\nIncomplete observations may omit blockers outside the observed window.\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestPipelineReadinessPrintsQueueBlockersAndActions(t *testing.T) {
	observedAt := "2026-09-01T07:00:00Z"
	nextRefreshAt := "2026-09-01T07:01:00Z"
	windowStartedAt := "2026-09-01T06:50:00Z"
	routedPercent := 80
	readiness := &buildkite.PipelineReadiness{
		Pipeline:             "monorepo",
		DestinationClusterID: "cluster-id",
		Status:               buildkite.PipelineReadinessBlocked,
		QueueObservation: buildkite.PipelineQueueObservation{
			ObservedAt:      &observedAt,
			NextRefreshAt:   &nextRefreshAt,
			WindowStartedAt: &windowStartedAt,
			WindowSeconds:   600,
			Complete:        false,
			BlockingQueues: []buildkite.PipelineBlockingQueue{
				{Queue: "deploy", Reasons: []string{"routing_incomplete", "active_source_jobs"}, RoutedPercent: &routedPercent, ActiveSourceJobs: 2},
				{Queue: "release", Reasons: []string{"migration_missing"}, RoutedPercent: nil, ActiveSourceJobs: 0},
			},
		},
		ConcurrencyGroupObservation: buildkite.PipelineConcurrencyGroupObservation{
			ObservedAt:      &observedAt,
			NextRefreshAt:   &nextRefreshAt,
			WindowStartedAt: &windowStartedAt,
			WindowSeconds:   600,
			Complete:        false,
		},
	}

	var output bytes.Buffer
	app := Context{Output: &output, Now: func() time.Time { return time.Date(2026, time.September, 1, 7, 0, 48, 0, time.UTC) }}
	if err := app.Print(readiness); err != nil {
		t.Fatal(err)
	}

	want := "PIPELINE READINESS\nPipeline: monorepo\nDestination cluster: cluster-id\nStatus: BLOCKED — 2 known blockers\n\nQUEUE BLOCKERS (2)\n\nQUEUE    ROUTING  ACTIVE SOURCE JOBS\ndeploy   80%      2\nrelease  —        0\n\ndeploy\n  - Routing is below 100% (routing_incomplete)\n  - Active jobs remain on the source queue (active_source_jobs)\n\nrelease\n  - No queue migration is configured (migration_missing)\n\nCONCURRENCY-GROUP BLOCKERS\nNone\n\nOBSERVATIONS\nQueues: incomplete; 10-minute window ending 48 seconds ago; started 2026-09-01 06:50:00 UTC\nQueue refresh eligible: in 12 seconds\nConcurrency groups: incomplete; 10-minute window ending 48 seconds ago; started 2026-09-01 06:50:00 UTC\nConcurrency-group refresh eligible: in 12 seconds\n\nIncomplete observations may omit blockers outside the observed window.\n\nNEXT STEPS\n\n1. Review destination activity for deploy:\n\n   cluster-migrator queue metrics deploy\n\n2. Wait for active source jobs on deploy to finish, then reassess:\n\n   cluster-migrator pipeline readiness monorepo --destination-cluster cluster-id\n\n3. Configure the missing queue migration for release:\n\n   cluster-migrator queue configure release --destination-cluster cluster-id\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestPipelineReadinessPrintsConcurrencyAndUnknownBlockers(t *testing.T) {
	readiness := &buildkite.PipelineReadiness{
		Pipeline:             "monorepo",
		DestinationClusterID: "cluster-id",
		Status:               buildkite.PipelineReadinessBlocked,
		QueueObservation: buildkite.PipelineQueueObservation{
			Complete: true,
		},
		ConcurrencyGroupObservation: buildkite.PipelineConcurrencyGroupObservation{
			Complete: true,
			BlockingConcurrencyGroups: []buildkite.BlockingConcurrencyGroup{
				{Scope: "pipeline", Key: "deploy", Reason: "concurrency_group_migration_unavailable"},
				{Scope: "", Key: "shared/deploy", Reason: "future_concurrency_reason"},
			},
		},
	}

	var output bytes.Buffer
	app := Context{Output: &output}
	if err := app.Print(readiness); err != nil {
		t.Fatal(err)
	}

	want := "PIPELINE READINESS\nPipeline: monorepo\nDestination cluster: cluster-id\nStatus: BLOCKED — 2 known blockers\n\nQUEUE BLOCKERS\nNone\n\nCONCURRENCY-GROUP BLOCKERS (2)\n\nSCOPE     KEY\npipeline  deploy\n—         shared/deploy\n\npipeline / deploy\n  - Migration is unavailable for this concurrency group (concurrency_group_migration_unavailable)\n\n— / shared/deploy\n  - Unknown blocker (future_concurrency_reason)\n\nOBSERVATIONS\nQueues: complete; window unavailable; observation time unavailable\nQueue refresh eligible: unavailable\nConcurrency groups: complete; window unavailable; observation time unavailable\nConcurrency-group refresh eligible: unavailable\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestPipelineReadinessActionsDeduplicateRepeatedReasons(t *testing.T) {
	readiness := &buildkite.PipelineReadiness{
		Pipeline:             "monorepo",
		DestinationClusterID: "cluster-id",
		QueueObservation: buildkite.PipelineQueueObservation{
			BlockingQueues: []buildkite.PipelineBlockingQueue{
				{Queue: "deploy", Reasons: []string{"routing_incomplete", "routing_incomplete"}},
			},
		},
	}

	actions := pipelineReadinessActions(readiness)
	if len(actions) != 1 {
		t.Fatalf("actions = %#v, want one unique action", actions)
	}
}

func TestPipelineReadinessPreservesUnknownStatus(t *testing.T) {
	if got, want := pipelineReadinessStatus("future_status", 0), "UNKNOWN (future_status)"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestFormatPipelineRefreshDueBoundaries(t *testing.T) {
	now := time.Date(2026, time.September, 1, 7, 1, 0, 0, time.UTC)
	tests := []struct {
		name          string
		nextRefreshAt string
		want          string
	}{
		{name: "past", nextRefreshAt: "2026-09-01T07:00:59.999Z", want: "now"},
		{name: "exact", nextRefreshAt: "2026-09-01T07:01:00Z", want: "now"},
		{name: "sub-second", nextRefreshAt: "2026-09-01T07:01:00.500Z", want: "in less than 1 second"},
		{name: "whole seconds", nextRefreshAt: "2026-09-01T07:01:59Z", want: "in 59 seconds"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := formatPipelineRefreshDue(&test.nextRefreshAt, now)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("refresh due = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPipelineReadinessJSONPreservesAPIShape(t *testing.T) {
	queueNextRefreshAt := "2026-09-01T07:01:00.123Z"
	concurrencyGroupNextRefreshAt := "2026-09-01T07:01:30.456Z"
	readiness := &buildkite.PipelineReadiness{
		Pipeline:             "monorepo",
		DestinationClusterID: "cluster-id",
		Status:               buildkite.PipelineReadinessBlocked,
		QueueObservation: buildkite.PipelineQueueObservation{
			NextRefreshAt: &queueNextRefreshAt,
			BlockingQueues: []buildkite.PipelineBlockingQueue{
				{Queue: "deploy", Reasons: []string{"migration_missing"}},
			},
		},
		ConcurrencyGroupObservation: buildkite.PipelineConcurrencyGroupObservation{
			NextRefreshAt:             &concurrencyGroupNextRefreshAt,
			BlockingConcurrencyGroups: []buildkite.BlockingConcurrencyGroup{},
		},
		URL: "https://api.buildkite.com/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness",
	}

	var output bytes.Buffer
	app := Context{Output: &output, JSON: true}
	if err := app.Print(readiness); err != nil {
		t.Fatal(err)
	}

	want := "{\n  \"pipeline\": \"monorepo\",\n  \"destination_cluster_id\": \"cluster-id\",\n  \"status\": \"blocked\",\n  \"retry_after_seconds\": null,\n  \"queue_observation\": {\n    \"observed_at\": null,\n    \"next_refresh_at\": \"2026-09-01T07:01:00.123Z\",\n    \"window_started_at\": null,\n    \"window_seconds\": 0,\n    \"complete\": false,\n    \"blocking_queues\": [\n      {\n        \"queue\": \"deploy\",\n        \"reasons\": [\n          \"migration_missing\"\n        ],\n        \"routed_percent\": null,\n        \"active_source_jobs\": 0\n      }\n    ]\n  },\n  \"concurrency_group_observation\": {\n    \"observed_at\": null,\n    \"next_refresh_at\": \"2026-09-01T07:01:30.456Z\",\n    \"window_started_at\": null,\n    \"window_seconds\": 0,\n    \"complete\": false,\n    \"blocking_concurrency_groups\": []\n  },\n  \"url\": \"https://api.buildkite.com/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness\"\n}\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}

	var decoded buildkite.PipelineReadiness
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
}

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
				_, _ = w.Write([]byte(`{"status":"observation_pending","retry_after_seconds":10,"queue_observation":{"next_refresh_at":null},"concurrency_group_observation":{"next_refresh_at":null}}`))
				return
			}
			_, _ = w.Write([]byte(`{"pipeline":"monorepo","destination_cluster_id":"cluster-id","status":"no_known_blockers","queue_observation":{"observed_at":"2026-09-01T07:00:00Z","next_refresh_at":"2026-09-01T07:01:00Z","window_started_at":"2026-09-01T06:50:00Z","window_seconds":600,"complete":false,"blocking_queues":[]},"concurrency_group_observation":{"observed_at":"2026-09-01T07:00:00Z","next_refresh_at":"2026-09-01T07:01:00Z","window_started_at":"2026-09-01T06:50:00Z","window_seconds":600,"complete":false,"blocking_concurrency_groups":[]}}`))
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
	if !strings.Contains(stdout.String(), "Status: NO KNOWN BLOCKERS — not proof of readiness") {
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
		_, _ = w.Write([]byte(`{"pipeline":"monorepo","destination_cluster_id":"cluster-id","status":"blocked","queue_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_queues":[{"queue":"deploy","reasons":["active_source_jobs"],"routed_percent":100,"active_source_jobs":1}]},"concurrency_group_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_concurrency_groups":[]}}`))
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
	if !strings.Contains(stdout.String(), "Status: BLOCKED — 1 known blocker") {
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
			_, _ = w.Write([]byte(`{"status":"no_known_blockers","queue_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_queues":[]},"concurrency_group_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_concurrency_groups":[]}}`))
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

func TestPipelineMoveDryRunReportsConcurrencyGroupBlockers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness":
			_, _ = w.Write([]byte(`{"status":"blocked","queue_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_queues":[]},"concurrency_group_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_concurrency_groups":[{"scope":"pipeline","key":"deploy","reason":"active_source_jobs"}]}}`))
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
	app := Context{Context: context.Background(), Client: client, Output: &stdout, ErrorOutput: &bytes.Buffer{}, DryRun: true, JSON: true}

	err = (&PipelineMoveCmd{Pipeline: "monorepo", DestinationCluster: "production"}).Run(&app)
	if err == nil || err.Error() != "pipeline has known blockers" {
		t.Fatalf("error = %v", err)
	}
	var assessment buildkite.PipelineReadiness
	decoder := json.NewDecoder(&stdout)
	if err := decoder.Decode(&assessment); err != nil {
		t.Fatalf("decode stdout: %v; stdout = %q", err, stdout.String())
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout contains more than one JSON document: %q", stdout.String())
	}
	if assessment.Status != buildkite.PipelineReadinessBlocked {
		t.Fatalf("status = %q", assessment.Status)
	}
	if got := assessment.ConcurrencyGroupObservation.BlockingConcurrencyGroups; len(got) != 1 || got[0].Scope != "pipeline" || got[0].Key != "deploy" || got[0].Reason != "active_source_jobs" {
		t.Fatalf("blocking concurrency groups = %#v", got)
	}
}

func TestPipelineMoveDryRunReportsQueueBlockers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
		case "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness":
			_, _ = w.Write([]byte(`{"status":"blocked","queue_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_queues":[{"queue":"deploy","reasons":["routing_incomplete","active_source_jobs"],"routed_percent":80,"active_source_jobs":2}]},"concurrency_group_observation":{"next_refresh_at":"2026-09-01T07:01:00Z","blocking_concurrency_groups":[]}}`))
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
	app := Context{Context: context.Background(), Client: client, Output: &stdout, ErrorOutput: &bytes.Buffer{}, DryRun: true, JSON: true}

	err = (&PipelineMoveCmd{Pipeline: "monorepo", DestinationCluster: "production"}).Run(&app)
	if err == nil || err.Error() != "pipeline has known blockers" {
		t.Fatalf("error = %v", err)
	}
	var assessment buildkite.PipelineReadiness
	if err := json.Unmarshal(stdout.Bytes(), &assessment); err != nil {
		t.Fatalf("decode stdout: %v; stdout = %q", err, stdout.String())
	}
	if got := assessment.QueueObservation.BlockingQueues; len(got) != 1 || got[0].Queue != "deploy" || len(got[0].Reasons) != 2 || got[0].RoutedPercent == nil || *got[0].RoutedPercent != 80 || got[0].ActiveSourceJobs != 2 {
		t.Fatalf("blocking queues = %#v", got)
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

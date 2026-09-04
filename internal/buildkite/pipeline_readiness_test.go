package buildkite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetPipelineReadiness(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/pipelines/monorepo/readiness" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("destination_cluster_id"); got != "cluster-id" {
			t.Fatalf("destination_cluster_id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"pipeline":"monorepo",
			"destination_cluster_id":"cluster-id",
			"status":"blocked",
			"retry_after_seconds":null,
			"queue_observation":{
				"observed_at":"2026-09-01T07:00:00Z",
				"next_refresh_at":"2026-09-01T07:01:00Z",
				"window_started_at":"2026-09-01T06:50:00Z",
				"window_seconds":600,
				"complete":false,
				"blocking_queues":[{
					"queue":"deploy",
					"reasons":["routing_incomplete","active_source_jobs"],
					"routed_percent":80,
					"active_source_jobs":2
				}]
			},
			"concurrency_group_observation":{
				"observed_at":"2026-09-01T07:00:00Z",
				"next_refresh_at":"2026-09-01T07:01:00Z",
				"window_started_at":"2026-09-01T06:50:00Z",
				"window_seconds":600,
				"complete":false,
				"blocking_concurrency_groups":[{
					"scope":"pipeline",
					"key":"deploy",
					"reason":"concurrency_group_migration_unavailable"
				}]
			}
		}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	readiness, err := client.GetPipelineReadiness(context.Background(), "monorepo", "cluster-id")
	if err != nil {
		t.Fatal(err)
	}
	if readiness.Status != PipelineReadinessBlocked {
		t.Fatalf("status = %q", readiness.Status)
	}
	if got := readiness.QueueObservation.BlockingQueues[0]; got.Queue != "deploy" || got.RoutedPercent == nil || *got.RoutedPercent != 80 || got.ActiveSourceJobs != 2 {
		t.Fatalf("blocking queue = %#v", got)
	}
	if got := readiness.ConcurrencyGroupObservation.BlockingConcurrencyGroups[0]; got.Scope != "pipeline" || got.Key != "deploy" {
		t.Fatalf("blocking concurrency group = %#v", got)
	}
	if readiness.QueueObservation.Complete || readiness.ConcurrencyGroupObservation.Complete {
		t.Fatal("observations should be incomplete")
	}
	if readiness.QueueObservation.NextRefreshAt == nil || *readiness.QueueObservation.NextRefreshAt != "2026-09-01T07:01:00Z" {
		t.Fatalf("queue next_refresh_at = %#v", readiness.QueueObservation.NextRefreshAt)
	}
	if readiness.ConcurrencyGroupObservation.NextRefreshAt == nil || *readiness.ConcurrencyGroupObservation.NextRefreshAt != "2026-09-01T07:01:00Z" {
		t.Fatalf("concurrency-group next_refresh_at = %#v", readiness.ConcurrencyGroupObservation.NextRefreshAt)
	}
}

func TestGetPipelineReadinessValidatesAvailableRefreshTimestamps(t *testing.T) {
	t.Parallel()

	valid := `{"status":"blocked","queue_observation":{"next_refresh_at":"2026-09-01T07:01:00Z"},"concurrency_group_observation":{"next_refresh_at":"2026-09-01T07:01:30Z"}}`
	tests := []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "divergent timestamps", response: valid},
		{name: "ready status missing queue timestamp", response: strings.Replace(strings.Replace(valid, `"status":"blocked"`, `"status":"no_known_blockers"`, 1), `"next_refresh_at":"2026-09-01T07:01:00Z"`, `"next_refresh_at":null`, 1), wantErr: "missing required queue_observation.next_refresh_at"},
		{name: "missing concurrency-group timestamp", response: strings.Replace(valid, `"next_refresh_at":"2026-09-01T07:01:30Z"`, `"next_refresh_at":null`, 1), wantErr: "missing required concurrency_group_observation.next_refresh_at"},
		{name: "invalid queue timestamp", response: strings.Replace(valid, "2026-09-01T07:01:00Z", "not-a-time", 1), wantErr: "parse queue_observation.next_refresh_at"},
		{name: "invalid concurrency-group timestamp", response: strings.Replace(valid, "2026-09-01T07:01:30Z", "not-a-time", 1), wantErr: "parse concurrency_group_observation.next_refresh_at"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			readiness, err := client.GetPipelineReadiness(context.Background(), "monorepo", "cluster-id")
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := *readiness.QueueObservation.NextRefreshAt; got != "2026-09-01T07:01:00Z" {
				t.Fatalf("queue next_refresh_at = %q", got)
			}
			if got := *readiness.ConcurrencyGroupObservation.NextRefreshAt; got != "2026-09-01T07:01:30Z" {
				t.Fatalf("concurrency-group next_refresh_at = %q", got)
			}
		})
	}
}

func TestGetPipelineReadinessAllowsPendingNullRefreshTimestamps(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"observation_pending","retry_after_seconds":10,"queue_observation":{"next_refresh_at":null},"concurrency_group_observation":{"next_refresh_at":null}}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	readiness, err := client.GetPipelineReadiness(context.Background(), "monorepo", "cluster-id")
	if err != nil {
		t.Fatal(err)
	}
	if readiness.RetryAfter == nil || *readiness.RetryAfter != 10 {
		t.Fatalf("retry_after_seconds = %#v", readiness.RetryAfter)
	}
	if readiness.QueueObservation.NextRefreshAt != nil || readiness.ConcurrencyGroupObservation.NextRefreshAt != nil {
		t.Fatalf("next_refresh_at = %#v / %#v, want nil / nil", readiness.QueueObservation.NextRefreshAt, readiness.ConcurrencyGroupObservation.NextRefreshAt)
	}
	encoded, err := json.Marshal(readiness)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(encoded), `"next_refresh_at":null`); got != 2 {
		t.Fatalf("JSON = %s, want two explicit null next_refresh_at fields", encoded)
	}
}

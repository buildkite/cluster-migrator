package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestQueueMetricsPrintsDestinationActivity(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	observedAt := time.Now().Add(-48 * time.Second).UTC().Format(time.RFC3339Nano)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/default/metrics" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"queue":"default",
			"destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"cluster-default"},
			"routed_percent":30,
			"window_started_at":"2026-09-01T06:50:00Z",
			"observed_at":%q,
			"window_seconds":600,
			"activity":{
				"connected_agents":{"current":50,"peak":54},
				"waiting_jobs":{"current":4,"peak":null},
				"running_jobs":{"current":null,"peak":46}
			}
		}`, observedAt)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--organization", "acme",
		"--endpoint", server.URL,
		"queue", "metrics", "default",
	}, &stdout, &bytes.Buffer{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	observedLine := regexp.MustCompile(`Observed: \d+ seconds ago`)
	got := observedLine.ReplaceAllString(stdout.String(), "Observed: <age>")
	want := "QUEUE ACTIVITY\nSource: default (unclustered)\nDestination: cluster-default (cluster cluster-id)\nRouting: 30%\nObserved: <age>\n\nMETRIC            LATEST  10M MAX\nConnected agents  50      54\nWaiting jobs      4       —\nRunning jobs      —       46\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueMetricsPrintsJSONWithMissingValues(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"queue":"default",
			"destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"default"},
			"routed_percent":30,
			"window_started_at":"2026-09-01T06:50:00Z",
			"observed_at":"2026-09-01T07:00:00Z",
			"window_seconds":600,
			"activity":{
				"connected_agents":{"current":50,"peak":54},
				"waiting_jobs":{"current":4,"peak":null},
				"running_jobs":{"current":38,"peak":46}
			}
		}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--organization", "acme",
		"--endpoint", server.URL,
		"--json",
		"queue", "metrics", "default",
	}, &stdout, &bytes.Buffer{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["observed_at"] != "2026-09-01T07:00:00Z" {
		t.Fatalf("observed_at = %v", got["observed_at"])
	}
	waitingJobs := got["activity"].(map[string]any)["waiting_jobs"].(map[string]any)
	if waitingJobs["peak"] != nil {
		t.Fatalf("waiting_jobs.peak = %v, want nil", waitingJobs["peak"])
	}
	if got["routed_percent"] != float64(30) {
		t.Fatalf("routed_percent = %v", got["routed_percent"])
	}
}

func TestQueueMetricsPrintsUnknownObservationWithoutError(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"queue":"default","destination":{"cluster_id":"cluster-id","queue_key":"default"},"routed_percent":30,"window_started_at":null,"observed_at":null,"window_seconds":600,"activity":{}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"--organization", "acme", "--endpoint", server.URL, "queue", "metrics", "default"}, &stdout, &bytes.Buffer{}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte("Observed: —\n")) {
		t.Fatalf("output = %q", got)
	}
}

func TestQueueMetricsRetriesBeforePrinting(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if requestCount < 3 {
			_, _ = w.Write([]byte(`{"retry_after_seconds":10}`))
			return
		}
		_, _ = w.Write([]byte(`{
			"queue":"default",
			"destination":{"cluster_id":"cluster-id","queue_key":"cluster-default"},
			"routed_percent":30,
			"observed_at":"2026-09-01T07:00:00Z",
			"window_seconds":600,
			"activity":{
				"connected_agents":{"current":50,"peak":54},
				"waiting_jobs":{"current":4,"peak":12},
				"running_jobs":{"current":38,"peak":46}
			}
		}`))
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
		Now:         func() time.Time { return time.Date(2026, time.September, 1, 7, 0, 48, 0, time.UTC) },
		Wait: func(ctx context.Context, duration time.Duration) error {
			waits++
			if duration != 10*time.Second {
				t.Fatalf("wait duration = %s, want 10s", duration)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout before final response = %q", stdout.String())
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("metrics retry context has no deadline")
			}
			return nil
		},
	}

	err = (&QueueMetricsCmd{Queue: "default"}).Run(&app)
	if err != nil {
		t.Fatal(err)
	}
	if waits != 2 || requestCount != 3 {
		t.Fatalf("waits/requests = %d/%d, want 2/3", waits, requestCount)
	}
	if got, want := stderr.String(), "Queue metrics are still being prepared; retrying every 10 seconds…\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte("Connected agents  50      54\n")) {
		t.Fatalf("stdout = %q", got)
	}
}

func TestQueueMetricsPreservesParentDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"retry_after_seconds":10}`))
	}))
	defer server.Close()

	client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	app := Context{
		Context:     ctx,
		Client:      client,
		Output:      &bytes.Buffer{},
		ErrorOutput: &bytes.Buffer{},
	}

	err = (&QueueMetricsCmd{Queue: "default"}).Run(&app)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want parent deadline exceeded", err)
	}
}

func TestFormatMetricsFreshness(t *testing.T) {
	now := time.Date(2026, time.September, 1, 7, 0, 48, 0, time.UTC)

	got, err := formatMetricsFreshness("2026-09-01T07:00:00Z", now)
	if err != nil {
		t.Fatal(err)
	}
	if got != "48 seconds ago" {
		t.Fatalf("freshness = %q, want %q", got, "48 seconds ago")
	}
}

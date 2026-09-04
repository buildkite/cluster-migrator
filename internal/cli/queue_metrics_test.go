package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
		"--endpoint", server.URL,
		"queue", "metrics", "default",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
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

func TestQueueMetricsPrintsSourceActivitySeparately(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	destinationObservedAt := time.Now().Add(-48 * time.Second).UTC().Format(time.RFC3339Nano)
	sourceObservedAt := time.Now().Add(-2 * time.Minute).UTC().Format(time.RFC3339Nano)
	nextRefreshAt := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"queue":"default",
			"destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"cluster-default"},
			"routed_percent":30,
			"window_started_at":"2026-09-01T06:50:00Z",
			"observed_at":%q,
			"next_refresh_at":%q,
			"window_seconds":600,
			"activity":{
				"connected_agents":{"current":50,"peak":54},
				"waiting_jobs":{"current":4,"peak":12},
				"running_jobs":{"current":38,"peak":46}
			},
			"source":{
				"queue_key":"default",
				"observed_at":%q,
				"activity":{
					"waiting_jobs":{"current":16,"peak":null},
					"running_jobs":{"current":null,"peak":null}
				}
			}
		}`, destinationObservedAt, nextRefreshAt, sourceObservedAt)
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"queue", "metrics", "default",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	sourceObservation := regexp.MustCompile(`SOURCE ACTIVITY \(Unclustered\)\nObserved: \d+ minutes ago`)
	destinationObservation := regexp.MustCompile(`DESTINATION ACTIVITY \(Cluster cluster-id\)\nObserved: \d+ seconds ago`)
	refreshDue := regexp.MustCompile(`Refresh due: in \d+ minute(?:s)?(?: \d+ seconds)?`)
	got := sourceObservation.ReplaceAllString(stdout.String(), "SOURCE ACTIVITY (Unclustered)\nObserved: <source age>")
	got = destinationObservation.ReplaceAllString(got, "DESTINATION ACTIVITY (Cluster cluster-id)\nObserved: <destination age>")
	got = refreshDue.ReplaceAllString(got, "Refresh due: <refresh due>")
	want := "QUEUE METRICS\nQueue: default\nRouting: 30%\n\nSOURCE ACTIVITY (Unclustered)\nObserved: <source age>\n\nMETRIC        CURRENT\nWaiting jobs  16\nRunning jobs  —\n\nDESTINATION ACTIVITY (Cluster cluster-id)\nObserved: <destination age>\nRefresh due: <refresh due>\n\nMETRIC            LATEST  10M MAX\nConnected agents  50      54\nWaiting jobs      4       12\nRunning jobs      38      46\n"
	if got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueMetricsPreservesSourceJSON(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	response := `{
		"queue":"default",
		"destination":{"cluster_id":"cluster-id","queue_id":"queue-id","queue_key":"default"},
		"routed_percent":30,
		"retry_after_seconds":null,
		"window_started_at":"2026-09-01T06:50:00Z",
		"observed_at":"2026-09-01T07:00:00Z",
		"window_seconds":600,
		"activity":{
			"connected_agents":{"current":50,"peak":54},
			"waiting_jobs":{"current":4,"peak":12},
			"running_jobs":{"current":38,"peak":46}
		},
		"source":{
			"queue_key":"default",
			"observed_at":"2026-09-01T07:00:48Z",
			"activity":{
				"waiting_jobs":{"current":16,"peak":null},
				"running_jobs":{"current":31,"peak":null}
			}
		}
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{
		"--endpoint", server.URL,
		"--json",
		"queue", "metrics", "default",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(response), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON = %#v, want %#v", got, want)
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
			"next_refresh_at":"2026-09-01T07:02:00Z",
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
		"--endpoint", server.URL,
		"--json",
		"queue", "metrics", "default",
	}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
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
	if got["next_refresh_at"] != "2026-09-01T07:02:00Z" {
		t.Fatalf("next_refresh_at = %v", got["next_refresh_at"])
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
	err := Run(context.Background(), []string{"--endpoint", server.URL, "queue", "metrics", "default"}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte("Observed: —\n")) {
		t.Fatalf("output = %q", got)
	}
	if got := stdout.String(); bytes.Contains([]byte(got), []byte("Refresh due:")) {
		t.Fatalf("output contains refresh due without next_refresh_at: %q", got)
	}
}

func TestQueueMetricsJSONOmitsAbsentNextRefreshAt(t *testing.T) {
	var stdout bytes.Buffer
	app := Context{Output: &stdout, JSON: true}

	if err := app.Print(&buildkite.QueueMetrics{}); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["next_refresh_at"]; ok {
		t.Fatalf("next_refresh_at should be absent: %s", stdout.String())
	}
}

func TestQueueMetricsPrintsRefreshDue(t *testing.T) {
	observedAt := "2026-09-01T07:00:00Z"
	nextRefreshAt := "2026-09-01T07:01:56Z"
	var stdout bytes.Buffer
	app := Context{
		Output: &stdout,
		Now: func() time.Time {
			return time.Date(2026, time.September, 1, 7, 0, 48, 0, time.UTC)
		},
	}

	err := app.printQueueMetrics(&buildkite.QueueMetrics{
		ObservedAt:    &observedAt,
		NextRefreshAt: &nextRefreshAt,
		WindowSeconds: 600,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !bytes.Contains([]byte(got), []byte("Observed: 48 seconds ago\nRefresh due: in 1 minute 8 seconds\n")) {
		t.Fatalf("output = %q", got)
	}
}

func TestQueueMetricsRejectsInvalidNextRefreshAt(t *testing.T) {
	nextRefreshAt := "not-a-timestamp"
	app := Context{
		Output: &bytes.Buffer{},
		Now:    func() time.Time { return time.Date(2026, time.September, 1, 7, 0, 0, 0, time.UTC) },
	}

	err := app.printQueueMetrics(&buildkite.QueueMetrics{NextRefreshAt: &nextRefreshAt})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("parse queue metrics next_refresh_at")) {
		t.Fatalf("error = %v", err)
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

	tests := []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{name: "just now", elapsed: 0, want: "just now"},
		{name: "less than one second", elapsed: 999 * time.Millisecond, want: "just now"},
		{name: "one second", elapsed: time.Second, want: "1 second ago"},
		{name: "seconds", elapsed: 59 * time.Second, want: "59 seconds ago"},
		{name: "exactly one minute", elapsed: time.Minute, want: "1 minute ago"},
		{name: "singular minute and second", elapsed: time.Minute + time.Second, want: "1 minute 1 second ago"},
		{name: "singular minute and plural seconds", elapsed: time.Minute + 2*time.Second, want: "1 minute 2 seconds ago"},
		{name: "plural minutes and singular second", elapsed: 2*time.Minute + time.Second, want: "2 minutes 1 second ago"},
		{name: "plural minutes and seconds", elapsed: 2*time.Minute + 2*time.Second, want: "2 minutes 2 seconds ago"},
		{name: "exact whole minutes", elapsed: 2 * time.Minute, want: "2 minutes ago"},
		{name: "last second before one hour", elapsed: 59*time.Minute + 59*time.Second, want: "59 minutes 59 seconds ago"},
		{name: "exactly one hour", elapsed: time.Hour, want: "1 hour ago"},
		{name: "whole hours", elapsed: 2 * time.Hour, want: "2 hours ago"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observedAt := now.Add(-test.elapsed).Format(time.RFC3339Nano)
			got, err := formatMetricsFreshness(observedAt, now)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("freshness = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormatMetricsRefreshDueClampsDueAndPastTimes(t *testing.T) {
	now := time.Date(2026, time.September, 1, 7, 2, 0, 0, time.UTC)

	for _, refreshAt := range []string{"2026-09-01T07:02:00Z", "2026-09-01T07:01:59Z"} {
		got, err := formatMetricsRefreshDue(refreshAt, now)
		if err != nil {
			t.Fatal(err)
		}
		if got != "now" {
			t.Fatalf("refresh due for %s = %q, want %q", refreshAt, got, "now")
		}
	}
}

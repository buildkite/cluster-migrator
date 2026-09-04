package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestQueueMetricsPrintsSourceActivitySeparately(t *testing.T) {
	now := time.Date(2026, time.September, 1, 7, 2, 0, 0, time.UTC)
	destinationObservedAt := "2026-09-01T07:00:08Z"
	sourceObservedAt := "2026-09-01T07:02:00Z"
	nextRefreshAt := "2026-09-01T07:02:08Z"
	routing, sourceAgents, sourceWaiting, sourceRunning := 30, 46, 16, 31
	destinationAgents, peakDestinationAgents := 50, 54
	destinationWaiting, peakDestinationWaiting := 4, 12
	destinationRunning, peakDestinationRunning := 38, 46
	currentWait, peakWait := 3.0, 6.5
	var stdout bytes.Buffer
	app := Context{Output: &stdout, Now: func() time.Time { return now }}
	err := app.printQueueMetrics(&buildkite.QueueMetrics{
		Queue:         "default",
		Destination:   buildkite.QueueDestination{ClusterID: "cluster-id"},
		RoutedPercent: &routing,
		ObservedAt:    &destinationObservedAt,
		NextRefreshAt: &nextRefreshAt,
		WindowSeconds: 600,
		Activity: buildkite.QueueActivity{
			ConnectedAgents:    buildkite.MetricValues{Current: &destinationAgents, Peak: &peakDestinationAgents},
			WaitingJobs:        buildkite.MetricValues{Current: &destinationWaiting, Peak: &peakDestinationWaiting},
			RunningJobs:        buildkite.MetricValues{Current: &destinationRunning, Peak: &peakDestinationRunning},
			WaitTimeP95Seconds: buildkite.DecimalMetricValues{Current: &currentWait, Peak: &peakWait},
		},
		Source: &buildkite.QueueSource{
			ObservedAt: &sourceObservedAt,
			Activity: buildkite.QueueSourceActivity{
				ConnectedAgents: buildkite.MetricValues{Current: &sourceAgents},
				WaitingJobs:     buildkite.MetricValues{Current: &sourceWaiting},
				RunningJobs:     buildkite.MetricValues{Current: &sourceRunning},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "QUEUE METRICS\n\nQueue: default    Routing: 30%\n\nSOURCE - Unclustered             DESTINATION - Cluster cluster-id\n+------------------+---------+   +------------------+--------+---------+\n| Metric           | Current |   | Metric           | Latest | 10m max |\n+------------------+---------+   +------------------+--------+---------+\n| Waiting jobs     |      16 |   | Waiting jobs     |      4 |      12 |\n| Running jobs     |      31 |   | Running jobs     |     38 |      46 |\n| Connected agents |      46 |   | Connected agents |     50 |      54 |\n+------------------+---------+   | Wait time (p95)  |     3s |    6.5s |\n                                 +------------------+--------+---------+\n\nSource observed: just now\nDestination refreshed: 1m 52s ago\nNext refresh eligible: in 8s\n"
	if got := stdout.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueMetricsStacksLongIdentityWithoutTruncatingIt(t *testing.T) {
	clusterID := strings.Repeat("destination-cluster-", 5)
	queue := strings.Repeat("source-queue-", 7)
	sourceAgents := 46
	var stdout bytes.Buffer
	app := Context{Output: &stdout}

	err := app.printQueueMetrics(&buildkite.QueueMetrics{
		Queue:         queue,
		Destination:   buildkite.QueueDestination{ClusterID: clusterID},
		WindowSeconds: 600,
		Source: &buildkite.QueueSource{Activity: buildkite.QueueSourceActivity{
			ConnectedAgents: buildkite.MetricValues{Current: &sourceAgents},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Queue: "+queue+"\nRouting: —\n") {
		t.Fatalf("long summary was not stacked intact: %q", got)
	}
	if !strings.Contains(got, "SOURCE - Unclustered\n+") || !strings.Contains(got, "\n\nDESTINATION - Cluster "+clusterID+"\n+") {
		t.Fatalf("long destination identity was not stacked intact: %q", got)
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
			"running_jobs":{"current":38,"peak":46},
			"wait_time_p95_seconds":{"current":3,"peak":6.5}
		},
		"source":{
			"queue_key":"default",
			"observed_at":"2026-09-01T07:00:48Z",
			"activity":{
				"connected_agents":{"current":46,"peak":null},
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

func TestQueueMetricsRejectsMissingSource(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")

	for _, test := range []struct {
		name     string
		response string
	}{
		{name: "omitted", response: `{"queue":"default"}`},
		{name: "null", response: `{"queue":"default","source":null}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()

			var stdout bytes.Buffer
			err := Run(context.Background(), []string{
				"--endpoint", server.URL,
				"--json",
				"queue", "metrics", "default",
			}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte("queue metrics response missing required source")) {
				t.Fatalf("error = %v", err)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
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
				"running_jobs":{"current":38,"peak":46},
				"wait_time_p95_seconds":{"current":null,"peak":null}
			},
			"source":{
				"queue_key":"default",
				"observed_at":"2026-09-01T07:00:48Z",
				"activity":{
					"connected_agents":{"current":46,"peak":null},
					"waiting_jobs":{"current":0,"peak":null},
					"running_jobs":{"current":0,"peak":null}
				}
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
	waitTime := got["activity"].(map[string]any)["wait_time_p95_seconds"].(map[string]any)
	if waitTime["current"] != nil || waitTime["peak"] != nil {
		t.Fatalf("wait_time_p95_seconds = %v, want null values", waitTime)
	}
	sourceAgents := got["source"].(map[string]any)["activity"].(map[string]any)["connected_agents"].(map[string]any)
	if sourceAgents["current"] != float64(46) || sourceAgents["peak"] != nil {
		t.Fatalf("source connected_agents = %v", sourceAgents)
	}
}

func TestQueueMetricsPrintsUnknownObservationWithoutError(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"queue":"default","destination":{"cluster_id":"cluster-id","queue_key":"default"},"routed_percent":30,"window_started_at":null,"observed_at":null,"window_seconds":600,"activity":{"connected_agents":{"current":null,"peak":null},"waiting_jobs":{"current":null,"peak":null},"running_jobs":{"current":null,"peak":null},"wait_time_p95_seconds":{"current":null,"peak":null}},"source":{"queue_key":"default","observed_at":"2026-09-01T07:00:00Z","activity":{"connected_agents":{"current":46,"peak":null},"waiting_jobs":{"current":0,"peak":null},"running_jobs":{"current":0,"peak":null}}}}`))
	}))
	defer server.Close()

	var stdout bytes.Buffer
	err := Run(context.Background(), []string{"--endpoint", server.URL, "queue", "metrics", "default"}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "Destination refreshed: —\n") {
		t.Fatalf("output = %q", got)
	}
	if got := stdout.String(); !strings.Contains(got, "| Connected agents |      46 |") || !strings.Contains(got, "| Wait time (p95)  |      — |       — |") {
		t.Fatalf("source activity missing from stale destination output: %q", got)
	}
	if got := stdout.String(); strings.Contains(got, "Next refresh eligible:") {
		t.Fatalf("output contains refresh due without next_refresh_at: %q", got)
	}
}

func TestQueueMetricsJSONOmitsAbsentNextRefreshAt(t *testing.T) {
	var stdout bytes.Buffer
	app := Context{Output: &stdout, JSON: true}
	source := &buildkite.QueueSource{}

	if err := app.Print(&buildkite.QueueMetrics{Source: source}); err != nil {
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
		Source:        &buildkite.QueueSource{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); !strings.Contains(got, "Destination refreshed: 48s ago\nNext refresh eligible: in 1m 8s\n") {
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
			_, _ = w.Write([]byte(`{"retry_after_seconds":10,"source":{"queue_key":"default","observed_at":"2026-09-01T07:00:48Z","activity":{"connected_agents":{"current":46,"peak":null},"waiting_jobs":{"current":0,"peak":null},"running_jobs":{"current":0,"peak":null}}}}`))
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
				"running_jobs":{"current":38,"peak":46},
				"wait_time_p95_seconds":{"current":3,"peak":6.5}
			},
			"source":{
				"queue_key":"default",
				"observed_at":"2026-09-01T07:00:48Z",
				"activity":{
					"connected_agents":{"current":46,"peak":null},
					"waiting_jobs":{"current":0,"peak":null},
					"running_jobs":{"current":0,"peak":null}
				}
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
	if got := stdout.String(); !strings.Contains(got, "| Connected agents |      46 |") || !strings.Contains(got, "| Connected agents |     50 |      54 |") {
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

func TestMetricDuration(t *testing.T) {
	zero := 0.0
	whole := 3.0
	fractional := 6.5
	precise := 0.125
	tests := []struct {
		name  string
		value *float64
		want  string
	}{
		{name: "unavailable", value: nil, want: "—"},
		{name: "zero", value: &zero, want: "0s"},
		{name: "whole", value: &whole, want: "3s"},
		{name: "fractional", value: &fractional, want: "6.5s"},
		{name: "precise fractional", value: &precise, want: "0.125s"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := metricDuration(test.value); got != test.want {
				t.Fatalf("duration = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompactMetricsTimes(t *testing.T) {
	now := time.Date(2026, time.September, 1, 7, 2, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{name: "future", elapsed: -time.Second, want: "just now"},
		{name: "subsecond", elapsed: 999 * time.Millisecond, want: "just now"},
		{name: "seconds", elapsed: 8 * time.Second, want: "8s ago"},
		{name: "minutes and seconds", elapsed: time.Minute + 52*time.Second, want: "1m 52s ago"},
		{name: "hours minutes seconds", elapsed: time.Hour + 2*time.Minute + 3*time.Second, want: "1h 2m 3s ago"},
	} {
		t.Run(test.name, func(t *testing.T) {
			observedAt := now.Add(-test.elapsed).Format(time.RFC3339Nano)
			got, err := formatCompactMetricsFreshness(observedAt, now)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("freshness = %q, want %q", got, test.want)
			}
		})
	}

	for _, test := range []struct {
		name      string
		remaining time.Duration
		want      string
	}{
		{name: "past", remaining: -time.Second, want: "now"},
		{name: "subsecond", remaining: 999 * time.Millisecond, want: "in <1s"},
		{name: "seconds", remaining: 8 * time.Second, want: "in 8s"},
		{name: "minutes and seconds", remaining: time.Minute + 8*time.Second, want: "in 1m 8s"},
	} {
		t.Run("refresh "+test.name, func(t *testing.T) {
			nextRefreshAt := now.Add(test.remaining).Format(time.RFC3339Nano)
			got, err := formatCompactMetricsRefreshDue(nextRefreshAt, now)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("refresh due = %q, want %q", got, test.want)
			}
		})
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

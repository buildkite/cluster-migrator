package buildkite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestQueueMetricsDecodesNestedContract(t *testing.T) {
	response := []byte(`{
		"queue":"default",
		"routed_percent":35,
		"retry_after_seconds":null,
		"destination":{
			"cluster_id":"cluster-id",
			"queue_id":"queue-id",
			"window_started_at":"2026-08-31T06:50:00Z",
			"observed_at":"2026-08-31T07:00:00Z",
			"next_refresh_at":"2026-08-31T07:01:00Z",
			"window_seconds":600,
			"activity":{
				"connected_agents":{"current":50,"peak":54},
				"waiting_jobs":{"current":4,"peak":12},
				"running_jobs":{"current":38,"peak":46},
				"wait_time_p95_seconds":{"current":12.5,"peak":18.2}
			}
		},
		"source":{
			"observed_at":"2026-08-31T07:00:48Z",
			"next_refresh_at":"2026-08-31T07:01:48Z",
			"activity":{
				"connected_agents":{"current":46,"peak":null},
				"waiting_jobs":{"current":16,"peak":null},
				"running_jobs":{"current":31,"peak":null}
			}
		}
	}`)

	var metrics QueueMetrics
	if err := json.Unmarshal(response, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.Destination == nil || metrics.Destination.Activity == nil {
		t.Fatalf("destination = %#v", metrics.Destination)
	}
	if metrics.Destination.Activity.WaitTimeP95Seconds.Current == nil || *metrics.Destination.Activity.WaitTimeP95Seconds.Current != 12.5 {
		t.Fatalf("current wait time = %v", metrics.Destination.Activity.WaitTimeP95Seconds.Current)
	}
	if metrics.Destination.Activity.WaitTimeP95Seconds.Peak == nil || *metrics.Destination.Activity.WaitTimeP95Seconds.Peak != 18.2 {
		t.Fatalf("peak wait time = %v", metrics.Destination.Activity.WaitTimeP95Seconds.Peak)
	}
	if metrics.Source == nil || metrics.Source.Activity == nil || metrics.Source.Activity.ConnectedAgents.Current == nil || *metrics.Source.Activity.ConnectedAgents.Current != 46 {
		t.Fatalf("source = %#v", metrics.Source)
	}
	if metrics.Source.Activity.ConnectedAgents.Peak != nil {
		t.Fatalf("source connected agents peak = %v, want nil", *metrics.Source.Activity.ConnectedAgents.Peak)
	}
}

func TestQueueMetricsUnavailableJSONPreservesNestedNulls(t *testing.T) {
	response := []byte(`{
		"queue":"default",
		"routed_percent":35,
		"retry_after_seconds":10,
		"destination":{
			"cluster_id":"cluster-id",
			"queue_id":"queue-id",
			"window_started_at":null,
			"observed_at":null,
			"next_refresh_at":"2026-08-31T07:00:58Z",
			"window_seconds":600,
			"activity":{
				"connected_agents":{"current":null,"peak":null},
				"waiting_jobs":{"current":null,"peak":null},
				"running_jobs":{"current":null,"peak":null},
				"wait_time_p95_seconds":{"current":null,"peak":null}
			}
		},
		"source":{
			"observed_at":"2026-08-31T07:00:48Z",
			"next_refresh_at":"2026-08-31T07:01:48Z",
			"activity":{
				"connected_agents":{"current":46,"peak":null},
				"waiting_jobs":{"current":16,"peak":null},
				"running_jobs":{"current":31,"peak":null}
			}
		}
	}`)

	var metrics QueueMetrics
	if err := json.Unmarshal(response, &metrics); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(&metrics)
	if err != nil {
		t.Fatal(err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON = %s, want %s", encoded, response)
	}
}

func TestGetQueueMetricsRejectsMissingRequiredNestedContract(t *testing.T) {
	valid := `"queue":"default","routed_percent":35,"retry_after_seconds":null,` +
		`"destination":{"cluster_id":"cluster-id","queue_id":"queue-id","window_started_at":null,"observed_at":null,"next_refresh_at":"2026-08-31T07:00:58Z","window_seconds":600,"activity":{}},` +
		`"source":{"observed_at":"2026-08-31T07:00:48Z","next_refresh_at":"2026-08-31T07:01:48Z","activity":{}}`

	for _, test := range []struct {
		name     string
		response string
		want     string
	}{
		{name: "destination", response: `{"source":{"observed_at":"x","next_refresh_at":"x","activity":{}}}`, want: "missing required destination"},
		{name: "source", response: `{"destination":{"next_refresh_at":"x","activity":{}}}`, want: "missing required source"},
		{name: "destination activity", response: `{` + strings.Replace(valid, `"activity":{}`, `"activity":null`, 1) + `}`, want: "missing required destination activity"},
		{name: "source activity", response: `{` + strings.Replace(valid, `"source":{"observed_at":"2026-08-31T07:00:48Z","next_refresh_at":"2026-08-31T07:01:48Z","activity":{}}`, `"source":{"observed_at":"2026-08-31T07:00:48Z","next_refresh_at":"2026-08-31T07:01:48Z","activity":null}`, 1) + `}`, want: "missing required source activity"},
		{name: "destination refresh", response: `{` + strings.Replace(valid, `"next_refresh_at":"2026-08-31T07:00:58Z"`, `"next_refresh_at":null`, 1) + `}`, want: "missing required destination next_refresh_at"},
		{name: "source observation", response: `{` + strings.Replace(valid, `"observed_at":"2026-08-31T07:00:48Z"`, `"observed_at":null`, 1) + `}`, want: "missing required source observed_at"},
		{name: "source refresh", response: `{` + strings.Replace(valid, `"next_refresh_at":"2026-08-31T07:01:48Z"`, `"next_refresh_at":null`, 1) + `}`, want: "missing required source next_refresh_at"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.GetQueueMetrics(context.Background(), "default")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

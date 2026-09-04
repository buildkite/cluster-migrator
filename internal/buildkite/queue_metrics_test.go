package buildkite

import (
	"encoding/json"
	"testing"
)

func TestQueueMetricsDecodesSourceAgentsAndFractionalWaitTime(t *testing.T) {
	response := []byte(`{
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
	}`)

	var metrics QueueMetrics
	if err := json.Unmarshal(response, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.Source == nil || metrics.Source.Activity.ConnectedAgents.Current == nil || *metrics.Source.Activity.ConnectedAgents.Current != 46 {
		t.Fatalf("source connected agents = %#v", metrics.Source)
	}
	if metrics.Source.Activity.ConnectedAgents.Peak != nil {
		t.Fatalf("source connected agents peak = %v, want nil", *metrics.Source.Activity.ConnectedAgents.Peak)
	}
	if metrics.Activity.WaitTimeP95Seconds.Current == nil || *metrics.Activity.WaitTimeP95Seconds.Current != 3 {
		t.Fatalf("current wait time = %v", metrics.Activity.WaitTimeP95Seconds.Current)
	}
	if metrics.Activity.WaitTimeP95Seconds.Peak == nil || *metrics.Activity.WaitTimeP95Seconds.Peak != 6.5 {
		t.Fatalf("peak wait time = %v", metrics.Activity.WaitTimeP95Seconds.Peak)
	}

	encoded, err := json.Marshal(&metrics)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	activity := got["activity"].(map[string]any)
	if activity["wait_time_p95_seconds"].(map[string]any)["peak"] != 6.5 {
		t.Fatalf("encoded wait time = %s", encoded)
	}
	sourceActivity := got["source"].(map[string]any)["activity"].(map[string]any)
	if sourceActivity["connected_agents"].(map[string]any)["peak"] != nil {
		t.Fatalf("encoded source agents = %s", encoded)
	}
}

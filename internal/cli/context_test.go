package cli

import (
	"bytes"
	"testing"
)

func TestQueueChangeJSONOmitsTerminalGuidance(t *testing.T) {
	from := 10
	var output bytes.Buffer
	app := Context{Output: &output, JSON: true}
	err := app.Print(QueueChange{
		Queue:       "default",
		FromPercent: &from,
		ToPercent:   25,
		Destination: QueueDestination{
			ClusterID:   "cluster-id",
			ClusterName: "Cluster Migrator Demo",
		},
		Next: "terminal-only guidance",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"queue\": \"default\",\n  \"from_percent\": 10,\n  \"to_percent\": 25,\n  \"destination\": {\n    \"cluster_id\": \"cluster-id\",\n    \"cluster_name\": \"Cluster Migrator Demo\"\n  },\n  \"dry_run\": false\n}\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestQueueStatusJSON(t *testing.T) {
	var output bytes.Buffer
	app := Context{Output: &output, JSON: true}
	err := app.Print(QueueStatus{
		Queue:          "default",
		RoutingPercent: 25,
		Destination: QueueDestination{
			ClusterID:   "cluster-id",
			ClusterName: "Cluster Migrator Demo",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"queue\": \"default\",\n  \"routing_percent\": 25,\n  \"destination\": {\n    \"cluster_id\": \"cluster-id\",\n    \"cluster_name\": \"Cluster Migrator Demo\"\n  }\n}\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

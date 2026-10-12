package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestConcurrencyGroupStatusOutput(t *testing.T) {
	t.Parallel()

	const (
		draining    = `{"scope":"custom","key":"deploy/production","state":"forward_draining","destination_cluster_id":"cluster-id","held_at":"2026-10-09T07:00:00.000Z","source_jobs":{"running":2,"waiting":3},"url":"https://api.buildkite.com/x","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`
		unclustered = `{"scope":"step","key":"deploy/production","state":"unclustered","destination_cluster_id":"cluster-id","held_at":null,"source_jobs":{"running":0,"waiting":1},"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`
		clustered   = `{"scope":"custom","key":"deploy/production","state":"clustered","destination_cluster_id":"cluster-id","held_at":null,"source_jobs":{"running":0,"waiting":0},"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`
		listed      = `{"scope":"custom","key":"deploy/production","state":"forward_draining","destination_cluster_id":"cluster-id","held_at":"2026-10-09T07:00:00.000Z","source_jobs":null,"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`
		listedStep  = `{"scope":"step","key":"deploy/production","state":"unclustered","destination_cluster_id":"cluster-id","held_at":null,"source_jobs":null,"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`
	)
	for _, test := range []struct {
		name         string
		group        string
		response     string
		wantJSON     string
		wantText     string
		wantClusters bool
	}{
		{
			name: "draining", group: "custom/deploy/production", response: draining, wantJSON: draining, wantClusters: true,
			wantText: "CONCURRENCY-GROUP STATUS\nGroup: custom/deploy/production\nDestination: production\n\nSTATUS\n\nState: forward_draining (cutover is waiting for running unclustered jobs to finish)\nHeld since: 2026-10-09 07:00:00 UTC\nRunning unclustered jobs: 2\nWaiting unclustered jobs: 3\n",
		},
		{
			name: "reversed", group: "step/deploy/production", response: unclustered, wantJSON: unclustered, wantClusters: true,
			wantText: "CONCURRENCY-GROUP STATUS\nGroup: step/deploy/production\nDestination: production\n\nSTATUS\n\nState: unclustered (moved back to unclustered queues)\nRunning unclustered jobs: 0\nWaiting unclustered jobs: 1\n",
		},
		{
			name: "clustered", group: "custom/deploy/production", response: clustered, wantJSON: clustered, wantClusters: true,
			wantText: "CONCURRENCY-GROUP STATUS\nGroup: custom/deploy/production\nDestination: production\n\nSTATUS\n\nState: clustered\nRunning unclustered jobs: 0\nWaiting unclustered jobs: 0\n\nNEXT STEPS\n\n1. Assess each pipeline using this concurrency group:\n\n   cluster-migrator pipeline readiness <pipeline> --destination-cluster cluster-id\n",
		},
		{
			name: "all", response: `{"items":[` + listedStep + `,` + listed + `],"links":{}}`, wantJSON: `[` + listedStep + `,` + listed + `]`, wantClusters: true,
			wantText: `CONCURRENCY-GROUP STATUS
Groups: 2

STATUS

GROUP                     STATE             DESTINATION  HELD SINCE
step/deploy/production    unclustered       production   —
custom/deploy/production  forward_draining  production   2026-10-09 07:00:00 UTC
`,
		},
		{
			name: "empty", response: `{"items":[],"links":{}}`, wantJSON: `[]`,
			wantText: "CONCURRENCY-GROUP STATUS\nGroups: 0\n\nSTATUS\n\nNo concurrency-group migrations found.\n",
		},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", test.name, jsonOutput), func(t *testing.T) {
				t.Parallel()
				clusterRequests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					path := "/v2/organizations/acme/concurrency-group-migrations"
					if test.group != "" {
						scope, key, _ := strings.Cut(test.group, "/")
						path += "/" + scope + "/" + strings.ReplaceAll(key, "/", "%2F")
					}
					w.Header().Set("Content-Type", "application/json")
					switch {
					case r.Method == http.MethodGet && r.URL.EscapedPath() == path:
						_, _ = w.Write([]byte(test.response))
					case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
						clusterRequests++
						_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.EscapedPath())
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer server.Close()
				client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
				if err != nil {
					t.Fatal(err)
				}
				var output bytes.Buffer
				app := &Context{Context: context.Background(), Client: client, Output: &output, JSON: jsonOutput}
				if err := (&ConcurrencyGroupStatusCmd{Group: test.group}).Run(app); err != nil {
					t.Fatal(err)
				}
				want := test.wantText
				if jsonOutput {
					var formatted bytes.Buffer
					if err := json.Indent(&formatted, []byte(test.wantJSON), "", "  "); err != nil {
						t.Fatal(err)
					}
					want = formatted.String() + "\n"
				}
				if got := output.String(); got != want {
					t.Fatalf("output = %q, want %q", got, want)
				}
				if wantRequests := map[bool]int{true: 1}[test.wantClusters && !jsonOutput]; clusterRequests != wantRequests {
					t.Fatalf("cluster requests = %d, want %d", clusterRequests, wantRequests)
				}
				t.Logf("stdout:\n%s", &output)
			})
		}
	}
}

func TestConcurrencyGroupStatusRejectsUnscopedGroups(t *testing.T) {
	t.Parallel()

	for _, group := range []string{"deploy", "pipeline/deploy", "custom/"} {
		err := (&ConcurrencyGroupStatusCmd{Group: group}).Run(&Context{Context: context.Background()})
		want := fmt.Sprintf("concurrency group %q must be <scope>/<key>, where scope is custom or step", group)
		if err == nil || err.Error() != want {
			t.Fatalf("group %q: error = %v, want %q", group, err, want)
		}
	}
}

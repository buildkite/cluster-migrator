package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestConcurrencyGroupStatusOutput(t *testing.T) {
	t.Parallel()

	group := `{"key":"deploy","state":"blocked","destination_cluster_id":"cluster-id","blocking_queues":["test","release"],"running_source_jobs":2,"waiting_jobs":3,"url":"https://buildkite.com/acme"}`
	other := `{"key":"release","state":"unclustered","destination_cluster_id":"","blocking_queues":[],"running_source_jobs":0,"waiting_jobs":0,"url":""}`
	clustered := `{"key":"deploy","state":"clustered","destination_cluster_id":"cluster-id","blocking_queues":[],"running_source_jobs":0,"waiting_jobs":0,"url":""}`
	draining := `{"key":"deploy","state":"draining","destination_cluster_id":"cluster-id","blocking_queues":[],"running_source_jobs":1,"waiting_jobs":0,"url":""}`
	wantGroup := `CONCURRENCY-GROUP STATUS
Group: deploy
Destination: production

STATUS

State: blocked
Running source jobs: 2
Waiting jobs: 3
URL: https://buildkite.com/acme

BLOCKING QUEUES

QUEUE
test
release

NEXT STEPS

1. Review the migration for each blocking queue:

   cluster-migrator queue status test
   cluster-migrator queue status release

2. If no queue blockers remain, preview the cutover:

   cluster-migrator concurrency-group cutover deploy --destination-cluster cluster-id --dry-run
`
	for _, test := range []struct {
		name     string
		key      string
		response string
		wantJSON string
		wantText string
	}{
		{name: "blocked", key: "deploy", response: group, wantJSON: group, wantText: wantGroup},
		{name: "unclustered", key: "release", response: other, wantJSON: other, wantText: "CONCURRENCY-GROUP STATUS\nGroup: release\nDestination: —\n\nSTATUS\n\nState: unclustered\nRunning source jobs: 0\nWaiting jobs: 0\n\nNEXT STEPS\n\n1. Preview the cutover:\n\n   cluster-migrator concurrency-group cutover release --destination-cluster <cluster> --dry-run\n"},
		{name: "draining", key: "deploy", response: draining, wantJSON: draining, wantText: "CONCURRENCY-GROUP STATUS\nGroup: deploy\nDestination: production\n\nSTATUS\n\nState: draining\nRunning source jobs: 1\nWaiting jobs: 0\n"},
		{name: "clustered", key: "deploy", response: clustered, wantJSON: clustered, wantText: "CONCURRENCY-GROUP STATUS\nGroup: deploy\nDestination: production\n\nSTATUS\n\nState: clustered\nRunning source jobs: 0\nWaiting jobs: 0\n\nNEXT STEPS\n\n1. Assess each pipeline using this concurrency group:\n\n   cluster-migrator pipeline readiness <pipeline> --destination-cluster cluster-id\n"},
		{name: "all", response: `{"items":[` + group + `,` + other + `],"links":{}}`, wantJSON: `[` + group + `,` + other + `]`, wantText: `CONCURRENCY-GROUP STATUS
Groups: 2

STATUS

GROUP    STATE        DESTINATION  RUNNING SOURCE JOBS  WAITING JOBS  BLOCKING QUEUES  URL
deploy   blocked      production   2                    3             test, release    https://buildkite.com/acme
release  unclustered  —            0                    0             —                —
`},
		{name: "empty", response: `{"items":[],"links":{}}`, wantJSON: `[]`, wantText: "CONCURRENCY-GROUP STATUS\nGroups: 0\n\nSTATUS\n\nNo concurrency groups found.\n"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", test.name, jsonOutput), func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					path := "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups"
					if test.key != "" {
						path += "/" + test.key
					}
					w.Header().Set("Content-Type", "application/json")
					switch {
					case r.Method == http.MethodGet && r.URL.Path == path:
						_, _ = w.Write([]byte(test.response))
					case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters" && !jsonOutput:
						_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
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
				cmd := ConcurrencyGroupStatusCmd{Group: test.key}
				if err := cmd.Run(app); err != nil {
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
				t.Logf("stdout:\n%s", &output)
			})
		}
	}
}

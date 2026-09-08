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
	wantGroup := `CONCURRENCY-GROUP STATUS
Group: deploy

STATUS

State: blocked
Destination cluster: cluster-id
Running source jobs: 2
Waiting jobs: 3
URL: https://buildkite.com/acme

BLOCKING QUEUES

QUEUE
test
release
`
	for _, test := range []struct {
		name     string
		key      string
		response string
		wantJSON string
		wantText string
	}{
		{name: "one", key: "deploy", response: group, wantJSON: group, wantText: wantGroup},
		{name: "no blockers", key: "release", response: other, wantJSON: other, wantText: "CONCURRENCY-GROUP STATUS\nGroup: release\n\nSTATUS\n\nState: unclustered\nDestination cluster: —\nRunning source jobs: 0\nWaiting jobs: 0\n"},
		{name: "all", response: `{"items":[` + group + `,` + other + `],"links":{}}`, wantJSON: `[` + group + `,` + other + `]`, wantText: `CONCURRENCY-GROUP STATUS
Groups: 2

STATUS

GROUP    STATE        DESTINATION  RUNNING SOURCE  WAITING  BLOCKING QUEUES  URL
deploy   blocked      cluster-id   2               3        test, release    https://buildkite.com/acme
release  unclustered  —            0               0        —                —
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
					if r.Method != http.MethodGet || r.URL.Path != path {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
						w.WriteHeader(http.StatusNotFound)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(test.response))
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

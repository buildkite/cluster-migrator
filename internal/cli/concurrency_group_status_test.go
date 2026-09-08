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
	wantGroup := "Group: deploy\nState: blocked\nDestination cluster: cluster-id\nRunning source jobs: 2\nWaiting jobs: 3\nBlocking queues: test, release\nURL: https://buildkite.com/acme\n"
	for _, test := range []struct {
		name     string
		key      string
		response string
		wantJSON string
		wantText string
	}{
		{name: "one", key: "deploy", response: group, wantJSON: group, wantText: "CONCURRENCY GROUP STATUS\nGroups: 1\n\n" + wantGroup},
		{name: "all", response: `{"items":[` + group + `,` + group + `],"links":{}}`, wantJSON: `[` + group + `,` + group + `]`, wantText: "CONCURRENCY GROUP STATUS\nGroups: 2\n\n" + wantGroup + "\n" + wantGroup},
		{name: "empty", response: `{"items":[],"links":{}}`, wantJSON: `[]`, wantText: "CONCURRENCY GROUP STATUS\nGroups: 0\n\nNo concurrency groups found.\n"},
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
			})
		}
	}
}

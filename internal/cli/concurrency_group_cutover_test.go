package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestConcurrencyGroupCutover(t *testing.T) {
	t.Parallel()

	const heading = "CONCURRENCY-GROUP CUTOVER\nGroup: deploy\nDestination: production\n"
	const dryHeading = "CONCURRENCY-GROUP CUTOVER (DRY RUN)\nGroup: deploy\nDestination: production\n"
	const status = "\nSTATUS\n\nState: %s\nDestination cluster: cluster-id\nRunning source jobs: 0\nWaiting jobs: 0\n"
	const next = "\nNEXT STEPS\n\n1. Inspect cutover status:\n\n   cluster-migrator concurrency-group status deploy\n"
	const blocked = "\nREADINESS\n\nCutover is blocked by queues.\n\nBLOCKING QUEUES\n\nQUEUE\ntest\nrelease\n"
	const response = `{"key":"deploy","state":%q,"destination_cluster_id":"cluster-id","blocking_queues":[],"running_source_jobs":0,"waiting_jobs":0,"url":""}`
	for _, test := range []struct {
		name      string
		dryRun    bool
		json      bool
		wait      bool
		blocked   bool
		state     string
		waitError error
		wantError string
		wantPosts int
		wantWaits int
		wantText  string
	}{
		{name: "non-interactive", wantPosts: 1, wantText: heading + "\nRESULT\n\nCutover request accepted.\n" + fmt.Sprintf(status, "draining") + next},
		{name: "JSON result", json: true, wantPosts: 1, wantText: fmt.Sprintf(response, "draining")},
		{name: "dry run", dryRun: true, wantText: dryHeading + "\nREADINESS\n\nNo known queue blockers. This is not proof of readiness.\n\nPROPOSED CHANGE\n\nA cutover to the production cluster would be requested.\n"},
		{name: "JSON dry run", json: true, dryRun: true, wantText: `{"action":"cut over","resource":"concurrency group deploy","destination_cluster":"production","dry_run":true}`},
		{name: "blocked", blocked: true, wantError: "concurrency group is blocked by queues: [test release]", wantText: heading + blocked},
		{name: "blocked dry run", dryRun: true, blocked: true, wantError: "concurrency group is blocked by queues: [test release]", wantText: dryHeading + blocked},
		{name: "JSON blocked dry run", json: true, dryRun: true, blocked: true, wantError: "concurrency group is blocked by queues: [test release]"},
		{name: "wait for completion", wait: true, state: "clustered", wantPosts: 1, wantWaits: 1, wantText: heading + "\nRESULT\n\nCutover completed.\n" + fmt.Sprintf(status, "clustered")},
		{name: "succeeded completion", wait: true, state: "succeeded", wantPosts: 1, wantWaits: 1, wantText: heading + "\nRESULT\n\nCutover completed.\n" + fmt.Sprintf(status, "succeeded")},
		{name: "JSON completion", json: true, wait: true, state: "clustered", wantPosts: 1, wantWaits: 1, wantText: fmt.Sprintf(response, "clustered")},
		{name: "failed cutover", wait: true, state: "failed", wantPosts: 1, wantWaits: 1, wantError: "concurrency-group cutover ended in failed"},
		{name: "cancelled cutover", wait: true, state: "cancelled", wantPosts: 1, wantWaits: 1, wantError: "concurrency-group cutover ended in cancelled"},
		{name: "cancelled wait", wait: true, waitError: context.Canceled, wantPosts: 1, wantWaits: 1, wantError: "context canceled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			posts, waits := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
					_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups/deploy":
					state := "draining"
					if waits > 0 {
						state = test.state
					}
					body := fmt.Sprintf(response, state)
					if test.blocked {
						body = strings.Replace(body, `"blocking_queues":[]`, `"blocking_queues":["test","release"]`, 1)
					}
					_, _ = fmt.Fprint(w, body)
				case r.Method == http.MethodPost && r.URL.Path == "/v2/organizations/acme/cluster-queue-migrations/concurrency-groups/deploy/cutover":
					posts++
					_, _ = fmt.Fprintf(w, response, "draining")
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
			var output, stderr bytes.Buffer
			app := &Context{
				Context: context.Background(), Client: client, Output: &output, ErrorOutput: &stderr, DryRun: test.dryRun, JSON: test.json,
				Wait: func(context.Context, time.Duration) error {
					waits++
					return test.waitError
				},
			}
			cmd := ConcurrencyGroupCutoverCmd{Group: "deploy", DestinationCluster: "production", Wait: test.wait}
			err = cmd.Run(app)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want %q", err, test.wantError)
				}
				if test.waitError != nil && !errors.Is(err, test.waitError) {
					t.Fatalf("error = %v, want %v", err, test.waitError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if posts != test.wantPosts || waits != test.wantWaits {
				t.Fatalf("posts/waits = %d/%d, want %d/%d", posts, waits, test.wantPosts, test.wantWaits)
			}
			want := test.wantText
			if test.json && want != "" {
				var formatted bytes.Buffer
				if err := json.Indent(&formatted, []byte(want), "", "  "); err != nil {
					t.Fatal(err)
				}
				want = formatted.String() + "\n"
			}
			if output.String() != want || stderr.Len() != 0 {
				t.Fatalf("stdout = %q, want %q; stderr = %q", output.String(), want, stderr.String())
			}
			t.Logf("stdout:\n%s", &output)
		})
	}
}

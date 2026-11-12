package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestConcurrencyGroupCutoverTimeout(t *testing.T) {
	for _, flags := range [][]string{nil, {"--json"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			posts := 0
			client := organizationClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body string
				switch {
				case request.Method == http.MethodGet && request.URL.Path == "/v2/organizations/acme/unclustered-to-clustered/migration":
					body = `{"destination_cluster_id":"cluster-id","phase":"cutover_concurrency_groups"}`
				case request.Method == http.MethodGet && request.URL.Path == "/v2/organizations/acme/clusters":
					body = `[{"id":"cluster-id","name":"production"}]`
				case request.Method == http.MethodPost && request.URL.EscapedPath() == "/v2/organizations/acme/unclustered-to-clustered/concurrency-group-migrations/custom/deploy%2Fproduction/cutover":
					posts++
					return nil, context.DeadlineExceeded
				default:
					t.Errorf("unexpected request: %s %s", request.Method, request.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})})
			var stdout, stderr bytes.Buffer
			args := append([]string{"concurrency-group", "cutover", "custom/deploy/production"}, flags...)
			err := Run(context.Background(), args, &stdout, &stderr, client)
			if !errors.Is(err, context.DeadlineExceeded) || stdout.Len() != 0 || stderr.Len() != 0 || posts != 1 {
				t.Fatalf("error=%v stdout=%q stderr=%q posts=%d; want deadline exceeded, empty output, and one POST", err, stdout.String(), stderr.String(), posts)
			}
			for _, guidance := range []string{"Mutation outcome is unknown", "Do not retry the mutation until you inspect its state", "cluster-migrator concurrency-group status custom/deploy/production"} {
				if !strings.Contains(err.Error(), guidance) {
					t.Errorf("error=%q, missing guidance %q", err, guidance)
				}
			}
		})
	}
}

func TestConcurrencyGroupCutover(t *testing.T) {
	t.Parallel()

	const (
		blocked = `{"message":"Concurrency group is not ready to cut over","scope":"custom","key":"deploy/production","destination_cluster_id":"cluster-id","ready":false,"status":"blocked",` +
			`"migration":{"state":"unclustered","blocking_reason":null},"queue_observation":{"observed_at":"2026-10-09T07:00:00Z","window_started_at":"2026-10-09T06:50:00Z","window_seconds":600,"queues":["deploy","release","test"],"blocking_queues":[` +
			`{"queue":"deploy","reasons":["recent_source_jobs"],"routed_percent":100,"recent_source_jobs":4},` +
			`{"queue":"release","reasons":["migration_missing"],"routed_percent":null,"recent_source_jobs":0},` +
			`{"queue":"test","reasons":["routing_incomplete"],"routed_percent":40,"recent_source_jobs":0}]}}`
		inProgress = `{"message":"Concurrency group is not ready to cut over","scope":"custom","key":"deploy/production","destination_cluster_id":"cluster-id","ready":false,"status":"blocked",` +
			`"migration":{"state":"forward_draining","blocking_reason":"cutover_in_progress"},"queue_observation":{"observed_at":"2026-10-09T07:00:00Z","window_started_at":"2026-10-09T06:50:00Z","window_seconds":600,"queues":["deploy"],"blocking_queues":[]}}`
		draining = `{"scope":"custom","key":"deploy/production","state":"forward_draining","destination_cluster_id":"cluster-id","held_at":"2026-10-09T07:00:00.000Z","source_jobs":{"running":1,"waiting":2},"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`

		heading      = "CONCURRENCY-GROUP CUTOVER\nGroup: custom/deploy/production\nDestination: production\n"
		observations = "\nOBSERVATIONS\n\nQueues used by this group: %s\nRecent jobs: 10-minute window ending 5 seconds ago; started 2026-10-09 06:50:00 UTC\n"
		statusNext   = "\nNEXT STEPS\n\n1. Check the concurrency group's migration:\n\n   cluster-migrator concurrency-group status custom/deploy/production\n"
	)
	blockedError := "concurrency group has known cutover blockers"

	for _, test := range []struct {
		name                         string
		json                         bool
		rejection                    string
		missingOrganizationMigration bool
		wantPosts                    int
		wantError                    string
		wantText                     string
	}{
		{name: "organization migration missing", missingOrganizationMigration: true, wantError: "get organization migration: Buildkite API returned 404: No migration found"},
		{
			name: "rejected by queues", rejection: blocked, wantPosts: 1, wantError: blockedError,
			wantText: heading + "\nRESULT\n\nCutover rejected without changes. Blocked by 3 known conditions.\n\nQUEUE BLOCKERS\n\n" +
				"QUEUE    ROUTING  RECENT UNGROUPED JOBS  BLOCKER\n" +
				"deploy   100%     4                      Ungrouped jobs were created on the unclustered queue recently.\n" +
				"release  —        0                      No queue migration is configured.\n" +
				"test     40%      0                      Routing is below 100%.\n" +
				strings.Replace(observations, "%s", "deploy, release, test", 1) +
				"\nNEXT STEPS\n\n" +
				"1. Wait until no ungrouped jobs have been created on the unclustered deploy queue for 10 minutes, then retry the cutover:\n\n   cluster-migrator concurrency-group cutover custom/deploy/production\n\n" +
				"2. Inspect the organization migration's phase before configuring the missing cluster queue migration for release:\n\n   cluster-migrator migration status\n\n" +
				"3. Review destination activity for test. Queue routing is frozen after commitment; contact Buildkite if it needs repair:\n\n   cluster-migrator queue metrics test\n",
		},
		{name: "JSON rejected", json: true, rejection: blocked, wantPosts: 1, wantError: blockedError, wantText: blocked},
		{
			name: "rejected while in progress", rejection: inProgress, wantPosts: 1, wantError: blockedError,
			wantText: heading + "\nRESULT\n\nCutover rejected without changes. Blocked by 1 known condition.\n\nMIGRATION BLOCKER\n\nState: forward_draining (cutover is waiting for running unclustered jobs to finish)\nBlocker: A cutover is already in progress.\n" +
				strings.Replace(observations, "%s", "deploy", 1) + statusNext,
		},
		{
			name: "started", wantPosts: 1,
			wantText: heading + "\nRESULT\n\nCutover to the production cluster started.\n\nSTATUS\n\nState: forward_draining (cutover is waiting for running unclustered jobs to finish)\nHeld since: 2026-10-09 07:00:00 UTC\nRunning unclustered jobs: 1\nWaiting unclustered jobs: 2\n" +
				"\nNEXT STEPS\n\n1. Check cutover progress:\n\n   cluster-migrator concurrency-group status custom/deploy/production\n",
		},
		{name: "JSON started", json: true, wantPosts: 1, wantText: draining},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const groupPath = "/v2/organizations/acme/unclustered-to-clustered/concurrency-group-migrations/custom/deploy%2Fproduction"
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/unclustered-to-clustered/migration":
					if test.missingOrganizationMigration {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"message":"No migration found"}`))
						return
					}
					_, _ = w.Write([]byte(`{"destination_cluster_id":"cluster-id","phase":"cutover_concurrency_groups"}`))
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
					_, _ = w.Write([]byte(`[{"id":"other-cluster-id","name":"development"},{"id":"cluster-id","name":"production"}]`))
				case r.Method == http.MethodPost && r.URL.EscapedPath() == groupPath+"/cutover":
					posts++
					if test.rejection != "" {
						w.WriteHeader(http.StatusConflict)
						_, _ = w.Write([]byte(test.rejection))
						return
					}
					w.WriteHeader(http.StatusAccepted)
					_, _ = w.Write([]byte(draining))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			app := &Context{
				Context: context.Background(), Client: client, Output: &stdout, ErrorOutput: &stderr, JSON: test.json,
				Now: func() time.Time { return time.Date(2026, 10, 9, 7, 0, 5, 0, time.UTC) },
			}
			err = (&ConcurrencyGroupCutoverCmd{Group: "custom/deploy/production"}).Run(app)
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || err.Error() != test.wantError) {
				t.Fatalf("error = %v, want %q", err, test.wantError)
			}
			if posts != test.wantPosts {
				t.Fatalf("posts = %d, want %d", posts, test.wantPosts)
			}
			want := test.wantText
			if test.json && want != "" {
				var formatted bytes.Buffer
				if err := json.Indent(&formatted, []byte(want), "", "  "); err != nil {
					t.Fatal(err)
				}
				want = formatted.String() + "\n"
			}
			if stdout.String() != want || stderr.Len() != 0 {
				t.Fatalf("stdout = %q, want %q; stderr = %q", stdout.String(), want, stderr.String())
			}
			t.Logf("stdout:\n%s", &stdout)
		})
	}
}

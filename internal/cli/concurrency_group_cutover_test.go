package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func TestConcurrencyGroupCutover(t *testing.T) {
	t.Parallel()

	const (
		blocked = `{"message":"Concurrency group is not ready to cut over","scope":"custom","key":"deploy/production","destination_cluster_id":"cluster-id","ready":false,"status":"blocked",` +
			`"migration":{"state":"unclustered","blocking_reason":null},"queue_observation":{"observed_at":"2026-10-09T07:00:00Z","window_started_at":"2026-10-09T06:50:00Z","window_seconds":600,"queues":["deploy","release"],"blocking_queues":[` +
			`{"queue":"deploy","reasons":["recent_source_jobs"],"routed_percent":100,"recent_source_jobs":4},` +
			`{"queue":"release","reasons":["migration_missing"],"routed_percent":null,"recent_source_jobs":0}]}}`
		inProgress = `{"message":"Concurrency group is not ready to cut over","scope":"custom","key":"deploy/production","destination_cluster_id":"cluster-id","ready":false,"status":"blocked",` +
			`"migration":{"state":"forward_draining","blocking_reason":"cutover_in_progress"},"queue_observation":{"observed_at":"2026-10-09T07:00:00Z","window_started_at":"2026-10-09T06:50:00Z","window_seconds":600,"queues":["deploy"],"blocking_queues":[]}}`
		draining    = `{"scope":"custom","key":"deploy/production","state":"forward_draining","destination_cluster_id":"cluster-id","held_at":"2026-10-09T07:00:00.000Z","source_jobs":{"running":1,"waiting":2},"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`
		clustered   = `{"scope":"custom","key":"deploy/production","state":"clustered","destination_cluster_id":"cluster-id","held_at":null,"source_jobs":{"running":0,"waiting":0},"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`
		unclustered = `{"scope":"custom","key":"deploy/production","state":"unclustered","destination_cluster_id":"%s","held_at":null,"source_jobs":{"running":0,"waiting":0},"url":"","created_at":"2026-10-09T06:59:00.000Z","updated_at":"2026-10-09T07:00:00.000Z"}`

		heading       = "CONCURRENCY-GROUP CUTOVER\nGroup: custom/deploy/production\nDestination: production\n"
		dryRunHeading = "CONCURRENCY-GROUP CUTOVER (DRY RUN)\nGroup: custom/deploy/production\nDestination: production\n"
		observations  = "\nOBSERVATIONS\n\nQueues used by this group: %s\nRecent jobs: 10-minute window ending 5 seconds ago; started 2026-10-09 06:50:00 UTC\n"
		statusNext    = "\nNEXT STEPS\n\n1. Check the concurrency group's migration:\n\n   cluster-migrator concurrency-group status custom/deploy/production\n"
		queuesChecked = " Queue routing and recent ungrouped jobs are checked when the cutover is requested.\n\nPROPOSED CHANGE\n\nA cutover to the production cluster would be requested.\n"
	)
	blockedError := "concurrency group has known cutover blockers"

	for _, test := range []struct {
		name                         string
		dryRun                       bool
		json                         bool
		existing                     string
		rejection                    string
		missingOrganizationMigration bool
		wantPosts                    int
		wantError                    string
		wantText                     string
	}{
		{name: "organization migration missing", missingOrganizationMigration: true, wantError: "get organization migration: Buildkite API returned 404: No migration found"},
		{name: "organization migration missing during dry run", dryRun: true, missingOrganizationMigration: true, wantError: "get organization migration: Buildkite API returned 404: No migration found"},
		{name: "dry run without a migration", dryRun: true, wantText: dryRunHeading + "\nREADINESS\n\nNo migration has started for this concurrency group." + queuesChecked},
		{name: "JSON dry run", dryRun: true, json: true, wantText: `{"action":"cut over","resource":"concurrency group custom/deploy/production","destination_cluster":"production","dry_run":true}`},
		{
			name: "dry run after a cancelled forward migration", dryRun: true, existing: strings.Replace(unclustered, "%s", "cluster-id", 1),
			wantText: dryRunHeading + "\nREADINESS\n\nThe concurrency group is unclustered and can cut over." + queuesChecked,
		},
		{
			name: "dry run while in progress", dryRun: true, existing: draining, wantError: blockedError,
			wantText: dryRunHeading + "\nREADINESS\n\nCutover is blocked by the concurrency group's migration.\n\nState: forward_draining (cutover is waiting for running unclustered jobs to finish)\nBlocker: A cutover is already in progress.\n" + statusNext,
		},
		{
			name: "dry run when already clustered", dryRun: true, existing: clustered, wantError: blockedError,
			wantText: dryRunHeading + "\nREADINESS\n\nCutover is blocked by the concurrency group's migration.\n\nState: clustered\nBlocker: The concurrency group has already moved to a cluster.\n" + statusNext,
		},
		{
			name: "dry run for another destination", dryRun: true, existing: strings.Replace(unclustered, "%s", "other-cluster-id", 1), wantError: blockedError,
			wantText: dryRunHeading + "\nREADINESS\n\nCutover is blocked by the concurrency group's migration.\n\nState: unclustered\nBlocker: The concurrency group's migration targets another cluster.\n" + statusNext,
		},
		{name: "JSON dry run while in progress", dryRun: true, json: true, existing: draining, wantError: "concurrency group cannot cut over: A cutover is already in progress."},
		{
			name: "rejected by queues", rejection: blocked, wantPosts: 1, wantError: blockedError,
			wantText: heading + "\nRESULT\n\nCutover rejected without changes. Blocked by 2 known conditions.\n\nQUEUE BLOCKERS\n\n" +
				"QUEUE    ROUTING  RECENT UNGROUPED JOBS  BLOCKER\n" +
				"deploy   100%     4                      Ungrouped jobs were created on the unclustered queue recently.\n" +
				"release  —        0                      No queue migration is configured.\n" +
				strings.Replace(observations, "%s", "deploy, release", 1) +
				"\nNEXT STEPS\n\n" +
				"1. Wait until no ungrouped jobs have been created on the unclustered deploy queue for 10 minutes, then retry the cutover:\n\n   cluster-migrator concurrency-group cutover custom/deploy/production\n\n" +
				"2. Configure the missing queue migration for release:\n\n   cluster-migrator queue configure release\n",
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
			const groupPath = "/v2/organizations/acme/concurrency-group-migrations/custom/deploy%2Fproduction"
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/unclustered-to-clustered-migration":
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
				case r.Method == http.MethodGet && r.URL.EscapedPath() == groupPath && posts == 0:
					if test.existing == "" {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"message":"No concurrency group migration found"}`))
						return
					}
					_, _ = w.Write([]byte(test.existing))
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
				Context: context.Background(), Client: client, Output: &stdout, ErrorOutput: &stderr, DryRun: test.dryRun, JSON: test.json,
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

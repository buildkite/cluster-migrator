package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationCommands(t *testing.T) {
	const path = "/v2/organizations/acme/unclustered-to-clustered-migration"
	const preparationNextSteps = `
NEXT STEPS

1. Review queue mappings:

   cluster-migrator queue status

2. Configure missing mappings and adjust routing while preparation is open.

3. Once the mappings are ready, start validation:

   cluster-migrator migration validate-queues
`
	const validationNextSteps = `
NEXT STEPS

1. Run representative workloads to check for unmapped source queues.

2. If you need to repair mappings or change routing, return to preparation first:

   cluster-migrator migration prepare-queues

3. After making those changes, start validation again:

   cluster-migrator migration validate-queues
`
	const concurrencyNextSteps = `
NEXT STEPS

1. Inspect concurrency-group migration progress:

   cluster-migrator concurrency-group status

2. If progress stalls or an operation fails, contact Buildkite. Queue configuration is frozen and preparation cannot be reopened.
`
	const pipelineNextSteps = `
NEXT STEPS

1. Coordinate the remaining pipeline moves with Buildkite. This CLI does not yet provide organization-wide pipeline cutover or migration completion.
`
	for _, test := range []struct {
		name      string
		operation string
		phase     string
		flags     []string
		status    int
		wantError string
		wantText  string
		wantNext  string
	}{
		{name: "status preparation", operation: "status", phase: "prepare_queues", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: prepare_queues\n", wantNext: preparationNextSteps},
		{name: "status validation", operation: "status", phase: "validate_queues", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: validate_queues\n", wantNext: validationNextSteps},
		{name: "status concurrency cutover", operation: "status", phase: "cutover_concurrency_groups", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: cutover_concurrency_groups\n", wantNext: concurrencyNextSteps},
		{name: "status pipeline migration", operation: "status", phase: "migrate_pipelines", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: migrate_pipelines\n", wantNext: pipelineNextSteps},
		{name: "status complete", operation: "status", phase: "complete", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: complete\n"},
		{name: "status unknown phase", operation: "status", phase: "future_phase", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: future_phase\n"},
		{name: "validate", operation: "validate-queues", phase: "validate_queues", wantText: "MIGRATION VALIDATE-QUEUES\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nQueue validation started. Unmapped source queues are rejected; queue mappings and routing percentages are frozen.\nConcurrency authority is unchanged.\n", wantNext: validationNextSteps},
		{name: "prepare", operation: "prepare-queues", phase: "prepare_queues", wantText: "MIGRATION PREPARE-QUEUES\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nQueue preparation reopened. Queue mappings and routing percentages can be changed; unmapped source queues are admitted again.\nConcurrency authority is unchanged.\n", wantNext: preparationNextSteps},
		{name: "JSON status", operation: "status", phase: "complete", flags: []string{"--json"}},
		{name: "JSON status validation", operation: "status", phase: "validate_queues", flags: []string{"--json"}},
		{name: "JSON validate", operation: "validate-queues", phase: "validate_queues", flags: []string{"--json"}},
		{name: "JSON prepare", operation: "prepare-queues", phase: "prepare_queues", flags: []string{"--json"}},
		{name: "validate preview", operation: "validate-queues", flags: []string{"--dry-run"}, wantError: "does not support --dry-run"},
		{name: "prepare JSON preview", operation: "prepare-queues", flags: []string{"--dry-run", "--json"}, wantError: "does not support --dry-run"},
		{name: "already validating", operation: "validate-queues", status: 409, wantError: "Buildkite API returned 409: Cannot transition"},
		{name: "committed", operation: "prepare-queues", status: 409, wantError: "Buildkite API returned 409: Cannot transition"},
		{name: "forbidden", operation: "prepare-queues", status: 403, wantError: "Buildkite API returned 403"},
		{name: "not configured", operation: "status", status: 404, wantError: "Buildkite API returned 404"},
		{name: "locking disabled", operation: "validate-queues", status: 503, wantError: "Buildkite API returned 503"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			requests := 0
			payload := fmt.Sprintf(`{"id":"migration-id","organization_id":"org-id","destination_cluster_id":"cluster-id","phase":%q,"created_by_id":"user-id","url":"https://api.buildkite.com/migration","created_at":"2026-10-26T00:00:00Z","updated_at":"2026-10-26T01:00:00Z"}`, test.phase)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				wantMethod, wantPath := http.MethodPost, path+"/"+test.operation
				if test.operation == "status" {
					wantMethod, wantPath = http.MethodGet, path
				}
				if r.Method != wantMethod || r.URL.Path != wantPath {
					t.Errorf("request = %s %s, want %s %s", r.Method, r.URL.Path, wantMethod, wantPath)
				}
				body, _ := io.ReadAll(r.Body)
				if len(body) != 0 {
					t.Errorf("unexpected request body: %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				if test.status != 0 {
					w.WriteHeader(test.status)
					_, _ = fmt.Fprint(w, `{"message":"Cannot transition"}`)
					return
				}
				_, _ = fmt.Fprint(w, payload)
			}))
			defer server.Close()

			var stdout, stderr bytes.Buffer
			args := append([]string{"--endpoint", server.URL, "migration", test.operation}, test.flags...)
			err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || stdout.Len() != 0 {
					t.Fatalf("error=%v stdout=%q, want %q and empty stdout", err, stdout.String(), test.wantError)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if test.wantText != "" {
					if want := test.wantText + test.wantNext; stdout.String() != want {
						t.Fatalf("stdout = %q, want %q", stdout.String(), want)
					}
				} else {
					var got, want any
					if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					_ = json.Unmarshal([]byte(payload), &want)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("JSON = %s, want %s", stdout.String(), payload)
					}
				}
			}
			wantRequests := 1
			if strings.Contains(test.name, "preview") {
				wantRequests = 0
			}
			if requests != wantRequests || stderr.Len() != 0 {
				t.Fatalf("requests=%d stderr=%q", requests, stderr.String())
			}
		})
	}
}

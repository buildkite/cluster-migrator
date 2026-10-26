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

func TestMigrationConfigure(t *testing.T) {
	const migrationPath = "/v2/organizations/acme/unclustered-to-clustered-migration"
	const payload = `{"id":"migration-id","organization_id":"org-id","destination_cluster_id":"cluster-id","phase":"prepare_queues","created_by_id":"user-id","url":"https://api.buildkite.com/migration","created_at":"2026-10-26T00:00:00Z","updated_at":"2026-10-26T00:00:00Z"}`
	const output = `MIGRATION CONFIGURE
Migration: migration-id
Destination: cluster-id

RESULT

Migration configured in queue preparation. No queue mappings or routing percentages were changed.

NEXT STEPS

1. Review queue mappings:

   cluster-migrator queue status

2. Configure missing mappings and adjust routing while preparation is open.

3. Once the mappings are ready, start validation:

   cluster-migrator migration validate-queues
`
	for _, test := range []struct {
		name        string
		destination string
		flags       []string
		clusters    string
		status      int
		message     string
		wantError   string
	}{
		{name: "create by name", destination: "production"},
		{name: "JSON create by ID", destination: "cluster-id", flags: []string{"--json"}},
		{name: "same destination conflict", destination: "cluster-id", status: 409, message: "This organization already has a migration", wantError: "configure organization migration: Buildkite API returned 409: This organization already has a migration; use cluster-migrator migration status to inspect it"},
		{name: "enrollment closed", destination: "production", status: 503, message: "Organization migration enrollment is not available yet", wantError: "configure organization migration: Buildkite API returned 503: Organization migration enrollment is not available yet"},
		{name: "permission denied", destination: "production", status: 403, message: "Forbidden", wantError: "configure organization migration: Buildkite API returned 403: Forbidden"},
		{name: "invalid target", destination: "production", status: 422, message: "Destination cluster must be active", wantError: "configure organization migration: Buildkite API returned 422: Destination cluster must be active"},
		{name: "missing cluster", destination: "missing", wantError: `resolve destination cluster: no cluster found matching "missing"`},
		{name: "ambiguous cluster", destination: "production", clusters: `[{"id":"one","name":"production"},{"id":"two","name":"production"}]`, wantError: `resolve destination cluster: multiple clusters match "production"`},
		{name: "dry run", destination: "production", flags: []string{"--dry-run"}, wantError: "migration configure does not support --dry-run: the server has no read-only readiness endpoint; no migration was configured"},
		{name: "JSON dry run", destination: "production", flags: []string{"--dry-run", "--json"}, wantError: "migration configure does not support --dry-run: the server has no read-only readiness endpoint; no migration was configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			posts, reads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing token authentication")
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters":
					reads++
					clusters := test.clusters
					if clusters == "" {
						clusters = `[{"id":"cluster-id","name":"production"}]`
					}
					_, _ = fmt.Fprint(w, clusters)
				case r.Method == http.MethodPost && r.URL.Path == migrationPath:
					posts++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if !reflect.DeepEqual(body, map[string]any{"destination_cluster_id": "cluster-id"}) || r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("unexpected configure payload: %v", body)
					}
					if test.status != 0 {
						w.WriteHeader(test.status)
						_ = json.NewEncoder(w).Encode(map[string]string{"message": test.message})
						return
					}
					w.WriteHeader(http.StatusCreated)
					_, _ = fmt.Fprint(w, payload)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			var stdout, stderr bytes.Buffer
			args := append([]string{"--endpoint", server.URL, "migration", "configure", "--destination-cluster", test.destination}, test.flags...)
			err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
			if test.wantError != "" {
				if err == nil || err.Error() != test.wantError || stdout.Len() != 0 {
					t.Fatalf("error=%v stdout=%q, want %q and empty stdout", err, stdout.String(), test.wantError)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(test.flags) != 0 {
					var got, want any
					if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					_ = json.Unmarshal([]byte(payload), &want)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("JSON = %s, want %s", stdout.String(), payload)
					}
				} else if stdout.String() != output {
					t.Fatalf("stdout = %q, want %q", stdout.String(), output)
				}
			}
			wantReads, wantPosts := 1, 1
			if strings.Contains(test.name, "dry run") {
				wantReads, wantPosts = 0, 0
			} else if test.name == "missing cluster" || test.name == "ambiguous cluster" {
				wantPosts = 0
			}
			if reads != wantReads || posts != wantPosts || stderr.Len() != 0 {
				t.Fatalf("reads=%d posts=%d stderr=%q; want reads=%d posts=%d and empty stderr", reads, posts, stderr.String(), wantReads, wantPosts)
			}
		})
	}
}

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

1. Coordinate pipeline cutover and completion with Buildkite. The server readiness checks for these transitions are not implemented yet.
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

func TestMigrationLifecycleTransitions(t *testing.T) {
	for _, operation := range []struct {
		name  string
		phase string
		flags []string
		body  string
	}{
		{"cutover-concurrency-groups", "cutover_concurrency_groups", []string{"--acknowledge-irreversible"}, `{"acknowledge_irreversible":true}`},
		{"cutover-pipelines", "migrate_pipelines", nil, ""},
		{"complete", "complete", nil, `{"finalize_blocked_builds":false}`},
		{"complete", "complete", []string{"--finalize-blocked-builds"}, `{"finalize_blocked_builds":true}`},
	} {
		t.Run(operation.name+strings.Join(operation.flags, ""), func(t *testing.T) {
			for _, outcome := range []struct {
				name    string
				flags   []string
				status  int
				message string
			}{
				{name: "accepted", status: 200},
				{name: "JSON accepted", flags: []string{"--json"}, status: 200},
				{name: "dry run", flags: []string{"--dry-run"}},
				{name: "JSON dry run", flags: []string{"--dry-run", "--json"}},
				{name: "not configured", status: 404, message: "Unclustered-to-clustered migration is not configured for this organization"},
				{name: "wrong phase", status: 409, message: "Cannot transition from prepare_queues"},
				{name: "invalid request", flags: []string{"--json"}, status: 422, message: map[string]string{
					"cutover-concurrency-groups": "acknowledge_irreversible must be true: commitment freezes queue mappings and routing percentages; preparation cannot be reopened",
					"cutover-pipelines":          "Invalid transition options",
					"complete":                   "finalize_blocked_builds must be a boolean",
				}[operation.name]},
				{name: "not implemented", status: 503, message: "Migration transition to " + operation.phase + " is not implemented yet"},
			} {
				t.Run(outcome.name, func(t *testing.T) {
					t.Setenv("BUILDKITE_API_TOKEN", "secret")
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					requests := 0
					payload := fmt.Sprintf(`{"id":"migration-id","organization_id":"org-id","destination_cluster_id":"cluster-id","phase":%q,"created_by_id":"user-id","url":"https://api.buildkite.com/migration","created_at":"2026-10-26T00:00:00Z","updated_at":"2026-10-26T01:00:00Z"}`, operation.phase)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests++
						if r.Method != http.MethodPost || r.URL.Path != "/v2/organizations/acme/unclustered-to-clustered-migration/"+operation.name {
							t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						}
						body, _ := io.ReadAll(r.Body)
						if operation.body == "" {
							if len(body) != 0 || r.Header.Get("Content-Type") != "" {
								t.Errorf("expected no body or Content-Type, got body=%q Content-Type=%q", body, r.Header.Get("Content-Type"))
							}
						} else {
							var got, want any
							if err := json.Unmarshal(body, &got); err != nil {
								t.Error(err)
							}
							_ = json.Unmarshal([]byte(operation.body), &want)
							if !reflect.DeepEqual(got, want) || r.Header.Get("Content-Type") != "application/json" {
								t.Errorf("unexpected JSON body or Content-Type: %s", body)
							}
						}
						if r.Header.Get("Authorization") != "Bearer secret" {
							t.Error("missing token authentication")
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(outcome.status)
						if outcome.message != "" {
							_ = json.NewEncoder(w).Encode(map[string]string{"message": outcome.message})
						} else {
							_, _ = fmt.Fprint(w, payload)
						}
					}))
					defer server.Close()

					var stdout, stderr bytes.Buffer
					args := append([]string{"--endpoint", server.URL, "migration", operation.name}, operation.flags...)
					args = append(args, outcome.flags...)
					err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
					wantError := ""
					wantRequests := 1
					if outcome.status == 0 {
						wantRequests = 0
						wantError = fmt.Sprintf("migration %s does not support --dry-run: the server has no read-only readiness endpoint; use cluster-migrator migration status to inspect the current phase", operation.name)
					} else if outcome.message != "" {
						wantError = fmt.Sprintf("migration %s: Buildkite API returned %d: %s", operation.name, outcome.status, outcome.message)
					}
					if wantError != "" {
						if err == nil || err.Error() != wantError || stdout.Len() != 0 {
							t.Fatalf("error=%v stdout=%q, want %q and empty stdout", err, stdout.String(), wantError)
						}
					} else if err != nil {
						t.Fatal(err)
					} else if outcome.name == "JSON accepted" {
						var got, want any
						if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
							t.Fatal(err)
						}
						_ = json.Unmarshal([]byte(payload), &want)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("JSON=%s, want %s", stdout.String(), payload)
						}
					} else {
						want := fmt.Sprintf("MIGRATION %s\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nPhase: %s\n\nNEXT STEPS\n\n1. Inspect the migration's current state:\n\n   cluster-migrator migration status\n", strings.ToUpper(operation.name), operation.phase)
						if stdout.String() != want {
							t.Fatalf("stdout=%q, want %q", stdout.String(), want)
						}
					}
					if requests != wantRequests || stderr.Len() != 0 {
						t.Fatalf("requests=%d stderr=%q, want requests=%d and empty stderr", requests, stderr.String(), wantRequests)
					}
				})
			}
		})
	}
}

func TestMigrationConcurrencyCutoverRequiresAcknowledgement(t *testing.T) {
	for _, flags := range [][]string{nil, {"--acknowledge-irreversible=false", "--json"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request without acknowledgement: %s %s", r.Method, r.URL)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := append([]string{"--endpoint", server.URL, "migration", "cutover-concurrency-groups"}, flags...)
			err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
			want := "migration cutover-concurrency-groups: requires --acknowledge-irreversible: commitment freezes queue mappings and routing percentages and prevents reopening preparation"
			if err == nil || err.Error() != want || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("error=%v stdout=%q stderr=%q, want %q and empty output", err, stdout.String(), stderr.String(), want)
			}
		})
	}
}

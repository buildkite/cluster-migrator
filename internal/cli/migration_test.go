package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestFinalizeBlockedBuildsTimeout(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requests := 0
	client := organizationClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/v2/organizations/acme/unclustered-to-clustered/migration/finalizations" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL)
		}
		return nil, context.DeadlineExceeded
	})})
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"finalize-blocked-builds", "--json"}, &stdout, &stderr, client)
	if !errors.Is(err, context.DeadlineExceeded) || stdout.Len() != 0 || stderr.Len() != 0 || requests != 1 {
		t.Fatalf("error=%v stdout=%q stderr=%q requests=%d, want deadline exceeded, empty output, and one request", err, stdout.String(), stderr.String(), requests)
	}
	for _, guidance := range []string{"the request may have been accepted", "a client timeout does not cancel server-side finalization", "run cluster-migrator migration status before retrying or requesting completion"} {
		if !strings.Contains(err.Error(), guidance) {
			t.Errorf("error=%q, missing guidance %q", err, guidance)
		}
	}
}

func TestMigrationCompletionTimeout(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requests := 0
	client := organizationClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/v2/organizations/acme/unclustered-to-clustered/migration/complete" {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL)
		}
		return nil, context.DeadlineExceeded
	})})
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"migration", "set-phase", "complete"}, &stdout, &stderr, client)
	if !errors.Is(err, context.DeadlineExceeded) || stdout.Len() != 0 || stderr.Len() != 0 || requests != 1 {
		t.Fatalf("error=%v stdout=%q stderr=%q requests=%d, want deadline exceeded, empty output, and one request", err, stdout.String(), stderr.String(), requests)
	}
	for _, guidance := range []string{"the request may have been accepted", "a client timeout does not cancel server-side work", "run cluster-migrator migration status before retrying"} {
		if !strings.Contains(err.Error(), guidance) {
			t.Errorf("error=%q, missing guidance %q", err, guidance)
		}
	}
}

func TestMigrationStatusCompletionBlockers(t *testing.T) {
	const path = "/v2/organizations/acme/unclustered-to-clustered/migration"
	const payload = `{"phase":"migrate_pipelines","target_phase":"complete","ready":false,"status":"blocked","blocker_sample_limit_per_category":20,"blocker_samples":[{"code":"unclustered_pipeline","message":"Pipeline 'monorepo' must be clustered","queue":null,"scope":null,"key":null},{"code":"unfinished_unclustered_build","message":"Unclustered build 'build-id' must be terminal (state: blocked); request blocked build finalization separately before completing the migration","queue":null,"scope":null,"key":null},{"code":"concurrency_group_migration_incomplete","message":"Concurrency group 'custom/deploy/production' must finish forward cutover and release its hold (state: forward_draining)","queue":null,"scope":"custom","key":"deploy/production"},{"code":"future_blocker","message":"Resolve the new server condition","queue":"release","scope":null,"key":null}]}`
	for _, flags := range [][]string{nil, {"--json"}, {"--dry-run"}} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.ContentLength > 0 || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case path:
					_, _ = fmt.Fprint(w, `{"id":"migration-id","destination_cluster_id":"cluster-id","phase":"migrate_pipelines"}`)
				case path + "/readiness":
					_, _ = fmt.Fprint(w, payload)
				default:
					t.Errorf("unexpected path: %s", r.URL)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := append([]string{"--endpoint", server.URL, "migration", "status"}, flags...)
			err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
			if err == nil || err.Error() != "migration has known completion blockers" || requests != 2 || stderr.Len() != 0 {
				t.Fatalf("error=%v requests=%d stderr=%q", err, requests, stderr.String())
			}
			if len(flags) > 0 && flags[0] == "--json" {
				var got map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				var want any
				_ = json.Unmarshal([]byte(payload), &want)
				if got["id"] != "migration-id" || !reflect.DeepEqual(got["completion_readiness"], want) {
					t.Fatalf("JSON lost server data: %s", stdout.String())
				}
				return
			}
			want := `MIGRATION STATUS
Migration: migration-id
Destination: cluster-id

STATUS

Phase: migrate_pipelines

COMPLETION READINESS

Target phase: complete
Completion is blocked.

BLOCKER SAMPLES

Up to 20 per category; these are samples, not totals.

CODE                                    BLOCKER
unclustered_pipeline                    Pipeline 'monorepo' must be clustered
unfinished_unclustered_build            Unclustered build 'build-id' must be terminal (state: blocked); request blocked build finalization separately before completing the migration
concurrency_group_migration_incomplete  Concurrency group 'custom/deploy/production' must finish forward cutover and release its hold (state: forward_draining)
future_blocker                          Resolve the new server condition

NEXT STEPS

1. Assess and move remaining unclustered pipelines individually, including archived pipelines. Start with:

   cluster-migrator pipeline readiness <pipeline>

2. Let running work finish normally. If blocked builds need finalization, request it separately:

   cluster-migrator finalize-blocked-builds

3. Inspect the concurrency group's progress:

   cluster-migrator concurrency-group status custom/deploy/production

4. Contact Buildkite to resolve the reported blocker. Queue configuration is frozen; preparation cannot be reopened.

5. Check completion readiness again after resolving blockers:

   cluster-migrator migration status
`
			if stdout.String() != want {
				t.Fatalf("stdout=%q, want %q", stdout.String(), want)
			}
		})
	}
}

func TestMigrationStatusReadinessErrors(t *testing.T) {
	for _, status := range []int{403, 404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet {
					t.Errorf("status must not mutate: %s", r.Method)
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/readiness") {
					w.WriteHeader(status)
					_, _ = fmt.Fprint(w, `{"message":"Readiness unavailable"}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"phase":"migrate_pipelines"}`)
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), []string{"--endpoint", server.URL, "migration", "status", "--json"}, &stdout, &stderr, organizationClient(server.Client()))
			want := fmt.Sprintf("get migration completion readiness: Buildkite API returned %d: Readiness unavailable", status)
			if err == nil || err.Error() != want || stdout.Len() != 0 || stderr.Len() != 0 || requests != 2 {
				t.Fatalf("error=%v stdout=%q stderr=%q requests=%d", err, stdout.String(), stderr.String(), requests)
			}
		})
	}
}

func TestMigrationConfigure(t *testing.T) {
	const migrationPath = "/v2/organizations/acme/unclustered-to-clustered/migration"
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

   cluster-migrator migration set-phase validate-queues
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
	const path = "/v2/organizations/acme/unclustered-to-clustered/migration"
	const preparationNextSteps = `
NEXT STEPS

1. Review queue mappings:

   cluster-migrator queue status

2. Configure missing mappings and adjust routing while preparation is open.

3. Once the mappings are ready, start validation:

   cluster-migrator migration set-phase validate-queues
`
	const validationNextSteps = `
NEXT STEPS

1. Run representative workloads to check for unmapped source queues.

2. If you need to repair mappings or change routing, return to preparation first:

   cluster-migrator migration set-phase prepare-queues

3. After making those changes, start validation again:

   cluster-migrator migration set-phase validate-queues

4. Once validation is satisfactory, all queues route 100%, and Buildkite confirms the safety gates, explicitly request irreversible commitment:

   cluster-migrator migration set-phase cutover-concurrency-groups --acknowledge-irreversible
`
	const concurrencyNextSteps = `
NEXT STEPS

1. Coordinate with Buildkite to identify required groups, then request each cutover separately:

   cluster-migrator concurrency-group cutover <scope>/<key>

2. Inspect concurrency-group migration progress. This lists migration records, not every group that needs cutover:

   cluster-migrator concurrency-group status

3. If progress stalls or an operation fails, contact Buildkite. Queue configuration is frozen and preparation cannot be reopened.

4. Once required group work has drained or moved and cutovers have finished and released their holds, request pipeline-phase entry. This does not move pipelines or finalize builds:

   cluster-migrator migration set-phase migrate-pipelines
`
	const completionReadiness = `{"phase":"migrate_pipelines","target_phase":"complete","ready":true,"status":"no_known_blockers","blocker_samples":[],"blocker_sample_limit_per_category":20}`
	const readyOutput = "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: migrate_pipelines\n\nCOMPLETION READINESS\n\nTarget phase: complete\nNo known completion blockers. This does not complete the migration.\n"
	const readyNextSteps = "\nNEXT STEPS\n\n1. Request completion explicitly. The server rechecks readiness when applying it:\n\n   cluster-migrator migration set-phase complete\n"
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
		{name: "status pipeline migration", operation: "status", phase: "migrate_pipelines", wantText: readyOutput, wantNext: readyNextSteps},
		{name: "status complete", operation: "status", phase: "complete", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: complete\n"},
		{name: "status unknown phase", operation: "status", phase: "future_phase", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: future_phase\n"},
		{name: "validate", operation: "validate-queues", phase: "validate_queues", wantText: "MIGRATION SET-PHASE VALIDATE-QUEUES\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nQueue validation started. Unmapped source queues are rejected; queue mappings and routing percentages are frozen.\nConcurrency authority is unchanged.\n", wantNext: validationNextSteps},
		{name: "prepare", operation: "prepare-queues", phase: "prepare_queues", wantText: "MIGRATION SET-PHASE PREPARE-QUEUES\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nQueue preparation reopened. Queue mappings and routing percentages can be changed; unmapped source queues are admitted again.\nConcurrency authority is unchanged.\n", wantNext: preparationNextSteps},
		{name: "JSON status", operation: "status", phase: "complete", flags: []string{"--json"}},
		{name: "JSON status validation", operation: "status", phase: "validate_queues", flags: []string{"--json"}},
		{name: "JSON status pipeline migration", operation: "status", phase: "migrate_pipelines", flags: []string{"--json"}},
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
				if test.operation == "status" && test.phase == "migrate_pipelines" && r.URL.Path == path+"/readiness" {
					if r.Method != http.MethodGet || r.ContentLength > 0 || r.Header.Get("Authorization") != "Bearer secret" {
						t.Errorf("unexpected readiness request: %s %s", r.Method, r.URL)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, completionReadiness)
					return
				}
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
			args := []string{"--endpoint", server.URL, "migration"}
			if test.operation != "status" {
				args = append(args, "set-phase")
			}
			args = append(args, test.operation)
			args = append(args, test.flags...)
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
					if test.operation == "status" && test.phase == "migrate_pipelines" {
						var readiness any
						_ = json.Unmarshal([]byte(completionReadiness), &readiness)
						want.(map[string]any)["completion_readiness"] = readiness
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("JSON = %s, want %s", stdout.String(), payload)
					}
				}
			}
			wantRequests := 1
			if test.operation == "status" && test.phase == "migrate_pipelines" {
				wantRequests++
			}
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
		path  string
		phase string
		flags []string
	}{
		{"cutover-concurrency-groups", "cutover-concurrency-groups", "cutover_concurrency_groups", []string{"--acknowledge-irreversible"}},
		{"migrate-pipelines", "cutover-pipelines", "migrate_pipelines", nil},
		{"complete", "complete", "complete", nil},
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
				{name: "invalid request", flags: []string{"--json"}, status: 422, message: "Invalid transition request"},
				{name: "not available", status: 503, message: "Migration transition to " + operation.phase + " is not available yet"},
			} {
				t.Run(outcome.name, func(t *testing.T) {
					t.Setenv("BUILDKITE_API_TOKEN", "secret")
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					requests := 0
					payload := fmt.Sprintf(`{"id":"migration-id","organization_id":"org-id","destination_cluster_id":"cluster-id","phase":%q,"created_by_id":"user-id","url":"https://api.buildkite.com/migration","created_at":"2026-10-26T00:00:00Z","updated_at":"2026-10-26T01:00:00Z"}`, operation.phase)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests++
						if r.Method != http.MethodPost || r.URL.Path != "/v2/organizations/acme/unclustered-to-clustered/migration/"+operation.path {
							t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						}
						body, _ := io.ReadAll(r.Body)
						if len(body) != 0 || r.Header.Get("Content-Type") != "" {
							t.Errorf("expected no body or Content-Type, got body=%q Content-Type=%q", body, r.Header.Get("Content-Type"))
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
					args := append([]string{"--endpoint", server.URL, "migration", "set-phase", operation.name}, operation.flags...)
					args = append(args, outcome.flags...)
					err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
					wantError := ""
					wantRequests := 1
					if outcome.status == 0 {
						wantRequests = 0
						wantError = fmt.Sprintf("migration set-phase %s does not support --dry-run; use cluster-migrator migration status to inspect the current phase and available completion readiness; no phase change was requested", operation.name)
					} else if outcome.message != "" {
						wantError = fmt.Sprintf("migration set-phase %s: Buildkite API returned %d: %s", operation.name, outcome.status, outcome.message)
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
						want := fmt.Sprintf("MIGRATION SET-PHASE %s\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nPhase: %s\n\nNEXT STEPS\n\n1. Inspect the migration's current state:\n\n   cluster-migrator migration status\n", strings.ToUpper(operation.name), operation.phase)
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

func TestMigrationTransitionsRequireAcknowledgement(t *testing.T) {
	for _, test := range []struct {
		operation string
		flag      string
		reason    string
	}{
		{"cutover-concurrency-groups", "--acknowledge-irreversible", "commitment freezes queue mappings and routing percentages and prevents reopening preparation"},
	} {
		for _, flags := range [][]string{nil, {test.flag + "=false", "--json"}} {
			t.Run(test.operation+strings.Join(flags, " "), func(t *testing.T) {
				t.Setenv("BUILDKITE_API_TOKEN", "secret")
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Errorf("unexpected request without acknowledgement: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusInternalServerError)
				}))
				defer server.Close()
				var stdout, stderr bytes.Buffer
				args := append([]string{"--endpoint", server.URL, "migration", "set-phase", test.operation}, flags...)
				err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
				want := fmt.Sprintf("migration set-phase %s: requires %s: %s", test.operation, test.flag, test.reason)
				if err == nil || err.Error() != want || stdout.Len() != 0 || stderr.Len() != 0 {
					t.Fatalf("error=%v stdout=%q stderr=%q, want %q and empty output", err, stdout.String(), stderr.String(), want)
				}
			})
		}
	}
}

func TestMigrationRejectsRemovedSyntax(t *testing.T) {
	for _, args := range [][]string{
		{"complete", "--finalize-blocked-builds"},
		{"set-phase", "complete", "--finalize-blocked-builds"},
		{"set-phase", "cutover-pipelines"},
		{"set-phase", "unknown"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), append([]string{"migration"}, args...), &stdout, &stderr, organizationClient(&http.Client{}))
			if err == nil || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("error=%v stdout=%q stderr=%q, want a parser error and empty stdout", err, stdout.String(), stderr.String())
			}
		})
	}
}

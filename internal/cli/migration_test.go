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

const migrationPath = "/v2/organizations/acme/unclustered-to-clustered-migration"
const migrationFixture = `{"id":"migration-id","organization_id":"org-id","destination_cluster_id":"cluster-id","phase":"validate_queues","created_by_id":"creator-id","url":"https://api.buildkite.com/v2/organizations/acme/unclustered-to-clustered-migration","created_at":"2026-10-22T01:00:00.000Z","updated_at":"2026-10-22T02:00:00.000Z","phase_version":"event-id","latest_transition":null}`
const readyFixture = `{"operation":"validate-queues","migration_id":"migration-id","organization_id":"org-id","phase":"validate_queues","phase_version":"event-id","transition":null,"ready":true,"status":"ready","blockers":[],"observed_at":"2026-10-22T02:00:00.000Z","expires_at":null,"cached":false}`

func TestMigrationLifecycleRequests(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	for _, test := range []struct {
		command string
		flags   []string
		body    map[string]any
	}{
		{command: "prepare-queues"},
		{command: "validate-queues"},
		{command: "cutover-concurrency-groups", flags: []string{"--acknowledge-irreversible"}, body: map[string]any{"acknowledge_irreversible": true}},
		{command: "cutover-pipelines"},
		{command: "complete"},
		{command: "complete", flags: []string{"--finalize-blocked-builds"}, body: map[string]any{"finalize_blocked_builds": true}},
	} {
		t.Run(test.command+strings.Join(test.flags, ""), func(t *testing.T) {
			posts := 0
			response := `{"outcome":"accepted","migration":` + migrationFixture + `,"readiness":` + readyFixture + `}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == migrationPath:
					_, _ = fmt.Fprint(w, migrationFixture)
				case r.Method == http.MethodPost && r.URL.Path == migrationPath+"/"+test.command:
					posts++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					want := map[string]any{"expected_phase_version": "event-id"}
					for key, value := range test.body {
						want[key] = value
					}
					if !reflect.DeepEqual(body, want) {
						t.Errorf("body = %#v, want %#v", body, want)
					}
					w.WriteHeader(http.StatusAccepted)
					_, _ = fmt.Fprint(w, response)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			args := append([]string{"--endpoint", server.URL, "--json", "migration", test.command}, test.flags...)
			if err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client())); err != nil {
				t.Fatal(err)
			}
			assertMigrationJSON(t, stdout.Bytes(), response)
			if posts != 1 || stderr.Len() != 0 {
				t.Fatalf("posts=%d stderr=%q", posts, &stderr)
			}
		})
	}
}

func TestMigrationPreviewsOnlyRead(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	for _, command := range []string{"configure", "prepare-queues", "validate-queues", "cutover-concurrency-groups", "cutover-pipelines", "complete", "complete-finalize"} {
		operation := command
		finalize := command == "complete-finalize"
		if finalize {
			operation = "complete"
		}
		for _, status := range []string{"ready", "blocked", "unavailable", "stale"} {
			t.Run(command+"/"+status, func(t *testing.T) {
				previews := 0
				response := strings.Replace(readyFixture, `"operation":"validate-queues"`, `"operation":"`+operation+`"`, 1)
				response = strings.Replace(response, `"status":"ready"`, `"status":"`+status+`"`, 1)
				if status != "ready" {
					response = strings.Replace(response, `"ready":true`, `"ready":false`, 1)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Errorf("preview sent mutation: %s %s", r.Method, r.URL)
					}
					switch r.URL.Path {
					case migrationPath:
						_, _ = fmt.Fprint(w, migrationFixture)
					case "/v2/organizations/acme/clusters":
						_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"production"}]`)
					case migrationPath + "/readiness":
						previews++
						if r.URL.Query().Get("operation") != operation || r.URL.Query().Has("acknowledge_irreversible") {
							t.Errorf("query=%s", r.URL.RawQuery)
						}
						if (r.URL.Query().Get("finalize_blocked_builds") == "true") != finalize {
							t.Errorf("finalization query=%s", r.URL.RawQuery)
						}
						if operation == "configure" && r.URL.Query().Get("destination_cluster_id") != "cluster-id" {
							t.Errorf("configure destination=%s", r.URL.RawQuery)
						}
						_, _ = fmt.Fprint(w, response)
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				args := []string{"--endpoint", server.URL, "--json", "--dry-run", "migration", operation}
				if operation == "configure" {
					args = append(args, "--destination-cluster", "production")
				}
				if finalize {
					args = append(args, "--finalize-blocked-builds")
				}
				var stdout, stderr bytes.Buffer
				err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
				if (err == nil) != (status == "ready") {
					t.Fatalf("status=%s error=%v", status, err)
				}
				assertMigrationJSON(t, stdout.Bytes(), response)
				if previews != 1 || stderr.Len() != 0 {
					t.Fatalf("previews=%d stderr=%q", previews, &stderr)
				}
			})
		}
	}
}

func TestMigrationCommitRequiresAcknowledgement(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		t.Errorf("unacknowledged commitment sent %s %s", request.Method, request.URL)
		return nil, fmt.Errorf("unexpected request")
	})}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"--api-token", "secret", "migration", "cutover-concurrency-groups"}, &stdout, &stderr, organizationClient(client))
	if err == nil || !strings.Contains(err.Error(), "--acknowledge-irreversible") || !strings.Contains(err.Error(), "freezes") || stdout.Len() != 0 {
		t.Fatalf("error=%v stdout=%q", err, &stdout)
	}
}

func TestMigrationOutcomes(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	for _, test := range []struct {
		outcome string
		status  int
		text    string
	}{
		{outcome: "accepted", status: 202, text: "Accepted. Background work is not complete."},
		{outcome: "completed", status: 200, text: "Operation completed."},
		{outcome: "no_op", status: 200, text: "No change was needed."},
		{outcome: "blocked", status: 409, text: "Operation blocked without authorization."},
	} {
		t.Run(test.outcome, func(t *testing.T) {
			response := `{"outcome":"` + test.outcome + `","migration":` + migrationFixture + `,"readiness":` + readyFixture + `}`
			if test.outcome == "blocked" {
				assessment := strings.Replace(readyFixture, `"ready":true,"status":"ready"`, `"ready":false,"status":"blocked"`, 1)
				response = `{"message":"Pipeline demo still has active source jobs","outcome":"blocked","readiness":` + strings.Replace(assessment, `"blockers":[]`, `"blockers":[{"code":"active_source_jobs","message":"Pipeline demo: wait for active source jobs to finish"}]`, 1) + `}`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == migrationPath {
					_, _ = fmt.Fprint(w, migrationFixture)
					return
				}
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, response)
			}))
			defer server.Close()
			for _, jsonOutput := range []bool{false, true} {
				var stdout, stderr bytes.Buffer
				args := []string{"--endpoint", server.URL, "migration", "validate-queues"}
				if jsonOutput {
					args = append(args, "--json")
				}
				err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
				if (err != nil) != (test.outcome == "blocked") {
					t.Fatalf("outcome=%s error=%v", test.outcome, err)
				}
				if jsonOutput {
					assertMigrationJSON(t, stdout.Bytes(), response)
				} else {
					status := "ready"
					blockers := ""
					if test.outcome == "blocked" {
						status = "blocked"
						blockers = "\nBLOCKERS\n\nCODE                BLOCKER\nactive_source_jobs  Pipeline demo: wait for active source jobs to finish\n"
					}
					want := "MIGRATION VALIDATE-QUEUES\nDestination: cluster-id\nPhase: validate_queues\n\nRESULT\n\n" + test.text + "\n\nREADINESS\n\nStatus: " + status + "\nAssessed phase: validate_queues\nObserved at: 2026-10-22T02:00:00.000Z\nExpires at: —\nCached: false\n" + blockers + "\nNEXT STEPS\n\n1. Inspect server-side progress and blockers; a client timeout does not cancel server-side work:\n\n   cluster-migrator migration status\n"
					if stdout.String() != want {
						t.Fatalf("stdout=%q, want %q", &stdout, want)
					}
				}
				if stderr.Len() != 0 {
					t.Fatalf("stderr=%q", &stderr)
				}
			}
		})
	}
}

func TestMigrationConfigurePreviewEnrollmentClosed(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	const response = `{"operation":"configure","migration_id":null,"organization_id":"org-id","phase":null,"phase_version":null,"transition":null,"ready":false,"status":"blocked","blockers":[{"code":"enrollment_closed","message":"Organization migration enrollment is not available yet"}],"observed_at":"2026-10-22T02:00:00.000Z","expires_at":null,"cached":false}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("preview sent %s %s", r.Method, r.URL)
		}
		switch r.URL.Path {
		case "/v2/organizations/acme/clusters":
			_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"production"}]`)
		case migrationPath + "/readiness":
			if r.URL.Query().Get("operation") != "configure" || r.URL.Query().Get("destination_cluster_id") != "cluster-id" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
			_, _ = fmt.Fprint(w, response)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, jsonOutput := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{"--endpoint", server.URL, "migration", "configure", "--destination-cluster", "production", "--dry-run"}
		if jsonOutput {
			args = append(args, "--json")
		}
		err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
		if err == nil || !strings.Contains(err.Error(), "preview is blocked; no authorization was recorded") || stderr.Len() != 0 {
			t.Fatalf("error=%v stderr=%q", err, &stderr)
		}
		if jsonOutput {
			assertMigrationJSON(t, stdout.Bytes(), response)
		} else {
			want := "MIGRATION CONFIGURE (DRY RUN)\nDestination: cluster-id\n\nREADINESS\n\nStatus: blocked\nAssessed phase: —\nObserved at: 2026-10-22T02:00:00.000Z\nExpires at: —\nCached: false\n\nBLOCKERS\n\nCODE               BLOCKER\nenrollment_closed  Organization migration enrollment is not available yet\n"
			if stdout.String() != want {
				t.Fatalf("stdout=%q, want %q", &stdout, want)
			}
		}
	}
}

func TestMigrationConfigurationAndStatus(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	for _, operation := range []string{"configure", "status"} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", operation, jsonOutput), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/v2/organizations/acme/clusters" {
						_, _ = fmt.Fprint(w, `[{"id":"cluster-id","name":"production"}]`)
						return
					}
					method := http.MethodGet
					if operation == "configure" {
						method = http.MethodPut
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !reflect.DeepEqual(body, map[string]any{"destination_cluster_id": "cluster-id"}) {
							t.Errorf("body=%#v error=%v", body, err)
						}
					}
					if r.Method != method || r.URL.Path != migrationPath {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					}
					_, _ = fmt.Fprint(w, migrationFixture)
				}))
				defer server.Close()
				args := []string{"--endpoint", server.URL, "migration", operation}
				if operation == "configure" {
					args = append(args, "--destination-cluster", "production")
				}
				if jsonOutput {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer
				if err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client())); err != nil {
					t.Fatal(err)
				}
				if jsonOutput {
					assertMigrationJSON(t, stdout.Bytes(), migrationFixture)
				} else {
					outcome := "\nAUDIT\n\nConfiguration creator: creator-id\nConfigured at: 2026-10-22T01:00:00.000Z\nPhase version: event-id\nLatest phase transition: not provided\n\nSTATUS\n\nBackground progress and blockers are not provided by this server.\n"
					if operation == "configure" {
						outcome = "\nRESULT\n\nOrganization migration is configured.\n"
					}
					want := "MIGRATION " + strings.ToUpper(operation) + "\nDestination: cluster-id\nPhase: validate_queues\n" + outcome + "\nNEXT STEPS\n\n1. To repair mappings or percentages, explicitly return to preparation:\n\n   cluster-migrator migration prepare-queues\n\n2. After validation and readiness checks, preview irreversible bulk commitment:\n\n   cluster-migrator migration cutover-concurrency-groups --dry-run\n"
					if stdout.String() != want {
						t.Fatalf("stdout=%q want=%q", &stdout, want)
					}
				}
				if stderr.Len() != 0 {
					t.Fatalf("stderr=%q", &stderr)
				}
			})
		}
	}
}

func TestMigrationRequiresCurrentPhaseVersion(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	for _, missingConfiguration := range []bool{false, true} {
		t.Run(fmt.Sprintf("unconfigured=%t", missingConfiguration), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != migrationPath {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if missingConfiguration {
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"message":"Unclustered-to-clustered migration is not configured for this organization"}`)
				} else {
					_, _ = fmt.Fprint(w, strings.Replace(migrationFixture, `,"phase_version":"event-id"`, "", 1))
				}
			}))
			defer server.Close()
			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), []string{"--endpoint", server.URL, "migration", "validate-queues"}, &stdout, &stderr, organizationClient(server.Client()))
			want := "phase_version"
			if missingConfiguration {
				want = "migration configure"
			}
			if err == nil || !strings.Contains(err.Error(), want) || stdout.Len() != 0 {
				t.Fatalf("error=%v stdout=%q", err, &stdout)
			}
		})
	}
}

func TestMigrationStatusTransitionAttribution(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	const transition = `{"event_id":"event-id","from":"cutover_concurrency_groups","to":"migrate_pipelines","source":"concurrency_group_completion","actor_id":null,"occurred_at":"2026-10-22T02:00:00.000Z"}`
	response := strings.Replace(migrationFixture, `"latest_transition":null`, `"latest_transition":`+transition, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != migrationPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		_, _ = fmt.Fprint(w, response)
	}))
	defer server.Close()
	for _, jsonOutput := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{"--endpoint", server.URL, "migration", "status"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client())); err != nil || stderr.Len() != 0 {
			t.Fatalf("error=%v stderr=%q", err, &stderr)
		}
		if jsonOutput {
			assertMigrationJSON(t, stdout.Bytes(), response)
			continue
		}
		want := "\nAUDIT\n\nConfiguration creator: creator-id\nConfigured at: 2026-10-22T01:00:00.000Z\nPhase version: event-id\nLatest phase transition: event-id\nTransition: cutover_concurrency_groups → migrate_pipelines\nSource: concurrency_group_completion\nActor: system\nOccurred at: 2026-10-22T02:00:00.000Z\n"
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout=%q, want %q", &stdout, want)
		}
	}
}

func TestMigrationStatusProgressIsSampledNotReadiness(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	progress := `{"concurrency_groups":{"authorization":{"event_id":"commit-id","authorized_at":"2026-10-22T01:00:00.000Z","actor_id":"actor-id"},"observation":{"status":"stale","observed_at":"2026-10-22T01:05:00.000Z","expires_at":"2026-10-22T01:06:00.000Z","cached":true},"sample":{"limit":50,"scanned":3,"has_more":true,"counts":{"remaining":2,"draining":1,"reconciling":1,"held":2,"completed":1,"blocked":0}},"items":[{"scope":"custom","key":"deploy","state":"forward_draining","held":true,"blockers":[]}]},"pipelines":{"authorization":null,"observation":{"status":"unavailable","observed_at":null,"expires_at":null,"cached":false},"sample":null,"items":[]}}`
	response := strings.TrimSuffix(migrationFixture, "}") + `,"progress":` + progress + `}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != migrationPath {
			t.Errorf("status sent %s %s", r.Method, r.URL)
		}
		_, _ = fmt.Fprint(w, response)
	}))
	defer server.Close()
	for _, jsonOutput := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{"--endpoint", server.URL, "migration", "status"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client())); err != nil {
			t.Fatal(err)
		}
		if jsonOutput {
			assertMigrationJSON(t, stdout.Bytes(), response)
			continue
		}
		for _, want := range []string{"Observation: stale", "Cached: true", "commit-id", "actor-id", "NOT ORGANIZATION TOTALS", "Scanned: 3", "More inventory: true", "Held overlaps draining and reconciling", "Sample unavailable", "Authorization: not recorded"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("stdout lacks %q:\n%s", want, &stdout)
			}
		}
		if strings.Contains(stdout.String(), "No known blockers") || strings.Contains(stdout.String(), "Total:") {
			t.Fatalf("sample implies readiness or totals:\n%s", &stdout)
		}
	}
}

func TestMigrationStatusPipelineBlockers(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "secret")
	const progress = `{"concurrency_groups":null,"pipelines":{"authorization":{"event_id":"authorization-id","authorized_at":"2026-10-22T02:00:00.000Z","actor_id":null},"observation":{"status":"fresh","observed_at":"2026-10-22T02:05:00.000Z","expires_at":"2026-10-22T02:06:00.000Z","cached":false},"sample":{"limit":50,"scanned":2,"has_more":true,"counts":{"remaining":1,"completed":1,"blocked":1,"incompatible":0}},"items":[{"pipeline_id":"pipeline-one","slug":"release","archived":true,"state":"blocked","blockers":[{"code":"active_source_jobs","message":"Wait for active source jobs to finish"}]},{"pipeline_id":"pipeline-two","slug":"test","archived":false,"state":"completed","blockers":[]}]}}`
	response := strings.TrimSuffix(migrationFixture, "}") + `,"progress":` + progress + `}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != migrationPath {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		_, _ = fmt.Fprint(w, response)
	}))
	defer server.Close()
	for _, jsonOutput := range []bool{false, true} {
		var stdout, stderr bytes.Buffer
		args := []string{"--endpoint", server.URL, "migration", "status"}
		if jsonOutput {
			args = append(args, "--json")
		}
		if err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client())); err != nil || stderr.Len() != 0 {
			t.Fatalf("error=%v stderr=%q", err, &stderr)
		}
		if jsonOutput {
			assertMigrationJSON(t, stdout.Bytes(), response)
			continue
		}
		for _, want := range []string{"Authorization event: authorization-id", "Actor: system", "Observation: fresh", "More inventory: true", "Completed means currently in the fixed destination", "release   pipeline-one  true      blocked    active_source_jobs: Wait for active source jobs to finish", "test      pipeline-two  false     completed  —"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("stdout lacks %q:\n%s", want, &stdout)
			}
		}
	}
}

func TestMigrationMutationTimeoutDoesNotImplyCancellation(t *testing.T) {
	posts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			posts++
			return nil, context.DeadlineExceeded
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(migrationFixture)), Header: make(http.Header)}, nil
	})}
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"--api-token", "secret", "migration", "cutover-pipelines"}, &stdout, &stderr, organizationClient(client))
	if err == nil || !strings.Contains(err.Error(), "server-side work may continue") || !strings.Contains(err.Error(), "migration status before retrying") || posts != 1 || stdout.Len() != 0 {
		t.Fatalf("error=%v posts=%d stdout=%q", err, posts, &stdout)
	}
}

func assertMigrationJSON(t *testing.T, got []byte, want string) {
	t.Helper()
	var actual, expected any
	if err := json.Unmarshal(got, &actual); err != nil {
		t.Fatalf("stdout is not one JSON value: %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("JSON=%s, want %s", got, want)
	}
}

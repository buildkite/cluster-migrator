package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrationProcess(t *testing.T) {
	const path = "/v2/organizations/acme/unclustered-to-clustered-migration"
	const migration = `{"id":"migration-id","organization_id":"org-id","destination_cluster_id":"cluster-id","phase":"validate_queues","phase_version":"event-id"}`
	const ready = `{"operation":"cutover-concurrency-groups","migration_id":"migration-id","organization_id":"org-id","phase":"validate_queues","phase_version":"event-id","transition":{"from":"validate_queues","to":"cutover_concurrency_groups"},"ready":true,"status":"ready","blockers":[],"observed_at":"2026-10-22T02:00:00.000Z","expires_at":null,"cached":false}`
	const staleMutation = `{"message":"Migration phase changed; refresh readiness before retrying"}`
	for _, test := range []struct {
		name       string
		args       []string
		status     int
		response   string
		wantPosts  int
		wantExit   int
		wantStdout string
		wantError  string
		wantEmpty  bool
	}{
		{name: "preview without acknowledgement", args: []string{"--dry-run"}, response: ready, wantStdout: "This preview does not authorize a transition or schedule work."},
		{name: "JSON preview", args: []string{"--dry-run", "--json"}, response: ready},
		{name: "accepted", args: []string{"--acknowledge-irreversible"}, status: 202, response: `{"outcome":"accepted","migration":` + migration + `,"readiness":` + ready + `}`, wantPosts: 1, wantStdout: "Accepted. Background work is not complete."},
		{name: "JSON blocked", args: []string{"--acknowledge-irreversible", "--json"}, status: 409, response: `{"message":"Queues are not ready","outcome":"blocked","readiness":` + strings.ReplaceAll(strings.ReplaceAll(ready, `"ready":true`, `"ready":false`), `"status":"ready"`, `"status":"blocked"`) + `}`, wantPosts: 1, wantExit: 1, wantError: "Queues are not ready"},
		{name: "stale mutation precondition", args: []string{"--acknowledge-irreversible"}, status: 409, response: staleMutation, wantPosts: 1, wantExit: 1, wantError: "Buildkite API returned 409: Migration phase changed; refresh readiness before retrying", wantEmpty: true},
		{name: "JSON stale mutation precondition", args: []string{"--acknowledge-irreversible", "--json"}, status: 409, response: staleMutation, wantPosts: 1, wantExit: 1, wantError: "Buildkite API returned 409: Migration phase changed; refresh readiness before retrying", wantEmpty: true},
		{name: "missing acknowledgement", wantExit: 1, wantError: "--acknowledge-irreversible is required", wantEmpty: true},
		{name: "false acknowledgement", args: []string{"--acknowledge-irreversible=false"}, wantExit: 1, wantError: "--acknowledge-irreversible is required", wantEmpty: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && posts > 0 {
					t.Errorf("unexpected read after mutation: %s", r.URL)
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/organizations":
					_, _ = fmt.Fprint(w, `[{"slug":"acme"}]`)
				case r.Method == http.MethodGet && r.URL.Path == path:
					_, _ = fmt.Fprint(w, migration)
				case r.Method == http.MethodGet && r.URL.Path == path+"/readiness":
					_, _ = fmt.Fprint(w, test.response)
				case r.Method == http.MethodPost && r.URL.Path == path+"/cutover-concurrency-groups":
					posts++
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body) != 2 || body["expected_phase_version"] != "event-id" || body["acknowledge_irreversible"] != true {
						t.Errorf("body=%v error=%v", body, err)
					}
					t.Logf("POST expected_phase_version=%v acknowledge_irreversible=%v", body["expected_phase_version"], body["acknowledge_irreversible"])
					w.WriteHeader(test.status)
					_, _ = fmt.Fprint(w, test.response)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			args := append([]string{"--endpoint", server.URL, "migration", "cutover-concurrency-groups"}, test.args...)
			stdout, stderr, exit := runCLI(t, args...)
			if exit != test.wantExit || posts != test.wantPosts || !strings.Contains(stdout, test.wantStdout) {
				t.Fatalf("exit=%d posts=%d stdout=%q stderr=%q", exit, posts, stdout, stderr)
			}
			if test.wantError != "" && !strings.Contains(stderr, test.wantError) || test.wantError == "" && stderr != "" {
				t.Fatalf("stderr=%q", stderr)
			}
			if !test.wantEmpty && strings.Contains(strings.Join(test.args, " "), "--json") && !json.Valid([]byte(stdout)) {
				t.Fatalf("not machine-only JSON: %q", stdout)
			}
			if test.wantEmpty && stdout != "" {
				t.Fatalf("stdout=%q", stdout)
			}
			t.Logf("exit=%d mutation requests=%d\nstdout:\n%sstderr:\n%s", exit, posts, stdout, stderr)
		})
	}
}

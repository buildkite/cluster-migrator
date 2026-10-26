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
	for _, test := range []struct {
		name      string
		operation string
		phase     string
		flags     []string
		status    int
		wantError string
		wantText  string
	}{
		{name: "status", operation: "status", phase: "prepare_queues", wantText: "MIGRATION STATUS\nMigration: migration-id\nDestination: cluster-id\n\nSTATUS\n\nPhase: prepare_queues\n"},
		{name: "validate", operation: "validate-queues", phase: "validate_queues", wantText: "MIGRATION VALIDATE-QUEUES\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nQueue validation started. Unmapped source queues are rejected; queue mappings and routing percentages are frozen.\nConcurrency authority is unchanged.\n"},
		{name: "prepare", operation: "prepare-queues", phase: "prepare_queues", wantText: "MIGRATION PREPARE-QUEUES\nMigration: migration-id\nDestination: cluster-id\n\nRESULT\n\nQueue preparation reopened. Queue mappings and routing percentages can be changed; unmapped source queues are admitted again.\nConcurrency authority is unchanged.\n"},
		{name: "JSON status", operation: "status", phase: "complete", flags: []string{"--json"}},
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
					if stdout.String() != test.wantText {
						t.Fatalf("stdout = %q, want %q", stdout.String(), test.wantText)
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

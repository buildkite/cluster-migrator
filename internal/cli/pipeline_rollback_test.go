package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const testPipelineRollbackResponse = `{"pipeline":"demo-pipeline","cluster_id":null,"assignment_changed":true,"cutoff":"2026-09-08T01:00:00Z","scanned_through":"2026-09-08T03:00:01Z","passes":2,"selected":0,"cancellation_enqueued":0,"pending":0,"failures":[],"best_effort":true}`

func TestPipelineRollbackResults(t *testing.T) {
	const contextOutput = `
CONTEXT

Fixed 2-hour lookback; two best-effort passes select up to 100 builds each. Older builds and late stale creators may be missed.
Cancellable states: creating, scheduled, started, failing, blocked. Already canceling builds count as pending; terminal builds (including failed) are excluded.
Cancellation enqueued does not mean jobs have stopped or concurrency slots are released.
Queue routing percentages and concurrency-group migration state remain unchanged.
`
	const nextSteps = `
NEXT STEPS

1. Inspect remaining builds and enqueue failures. Verify jobs and concurrency slots separately.
2. If cleanup is still needed, rerun rollback, even when already unclustered. Each run uses a new 2-hour cutoff:

   cluster-migrator pipeline rollback demo-pipeline
`
	for _, test := range []struct {
		name         string
		response     string
		assignment   string
		selected     int
		enqueued     int
		pending      int
		failures     int
		failureTable string
	}{
		{name: "clear", response: testPipelineRollbackResponse, assignment: "Pipeline cluster assignment cleared."},
		{name: "already unclustered", response: strings.Replace(testPipelineRollbackResponse, `"assignment_changed":true`, `"assignment_changed":false`, 1), assignment: "Pipeline was already unclustered; residual cleanup attempted."},
		{name: "pending", response: strings.Replace(testPipelineRollbackResponse, `"pending":0`, `"pending":250`, 1), assignment: "Pipeline cluster assignment cleared.", pending: 250},
		{name: "enqueued", response: strings.Replace(strings.Replace(testPipelineRollbackResponse, `"selected":0`, `"selected":1`, 1), `"cancellation_enqueued":0`, `"cancellation_enqueued":1`, 1), assignment: "Pipeline cluster assignment cleared.", selected: 1, enqueued: 1},
		{name: "failure", response: strings.Replace(strings.Replace(testPipelineRollbackResponse, `"selected":0`, `"selected":1`, 1), `"failures":[]`, `"failures":[{"build_uuid":"build-uuid","message":"Enqueue failed"}]`, 1), assignment: "Pipeline cluster assignment cleared.", selected: 1, failures: 1, failureTable: "\nENQUEUE FAILURES\n\nBUILD       ERROR\nbuild-uuid  Enqueue failed\n"},
		{name: "multiple failures", response: strings.Replace(strings.Replace(testPipelineRollbackResponse, `"selected":0`, `"selected":2`, 1), `"failures":[]`, `"failures":[{"build_uuid":"build-uuid","message":"Enqueue failed"},{"build_uuid":"longer-build-uuid","message":"Try again later"}]`, 1), assignment: "Pipeline cluster assignment cleared.", selected: 2, failures: 2, failureTable: "\nENQUEUE FAILURES\n\nBUILD              ERROR\nbuild-uuid         Enqueue failed\nlonger-build-uuid  Try again later\n"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", test.name, jsonOutput), func(t *testing.T) {
				t.Setenv("BUILDKITE_API_TOKEN", "secret")
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.Method != http.MethodPost || r.URL.Path != "/v2/organizations/acme/cluster-queue-migrations/pipelines/demo-pipeline/rollback" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					}
					_, _ = fmt.Fprint(w, test.response)
				}))
				defer server.Close()
				args := []string{"--endpoint", server.URL, "pipeline", "rollback", "demo-pipeline"}
				if jsonOutput {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer
				err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client()))
				wantErr := test.pending > 0 || test.failures > 0
				if (err != nil) != wantErr || (err != nil && !strings.Contains(err.Error(), "cleanup incomplete")) {
					t.Fatalf("error = %v, wantErr = %t", err, wantErr)
				}
				if requests != 1 || stderr.Len() != 0 {
					t.Fatalf("requests=%d stderr=%q", requests, stderr.String())
				}
				if jsonOutput {
					var got, want any
					if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal([]byte(test.response), &want); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("output = %s, want %s", &stdout, test.response)
					}
				} else {
					want := fmt.Sprintf(`PIPELINE ROLLBACK
Pipeline: demo-pipeline

RESULT

%s

CLEANUP

Cutoff (inclusive): 2026-09-08T01:00:00Z
Scanned through: 2026-09-08T03:00:01Z
Passes: 2
Selected: %d
Cancellation enqueued: %d
Still-live clustered builds in window: %d
Enqueue failures: %d
`, test.assignment, test.selected, test.enqueued, test.pending, test.failures)
					if test.selected == 0 {
						want += "\nNo builds were selected for cancellation.\n"
					}
					if test.pending == 0 {
						want += "\nNo pending builds observed in this window; this is not proof of complete cleanup.\n"
					}
					want += test.failureTable + contextOutput
					if wantErr {
						want += nextSteps
					}
					if stdout.String() != want {
						t.Fatalf("output = %q, want %q", stdout.String(), want)
					}
					t.Logf("stdout:\n%s", &stdout)
				}
			})
		}
	}
}

func TestPipelineRollbackDryRun(t *testing.T) {
	for _, jsonOutput := range []bool{false, true} {
		for _, clusterID := range []string{`"cluster-id"`, `null`} {
			t.Run(fmt.Sprintf("json=%t cluster=%s", jsonOutput, clusterID), func(t *testing.T) {
				t.Setenv("BUILDKITE_API_TOKEN", "secret")
				var requests []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests = append(requests, r.Method+" "+r.URL.RequestURI())
					if r.Method != http.MethodGet || r.URL.Path != "/v2/organizations/acme/pipelines/demo-pipeline" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					_, _ = fmt.Fprintf(w, `{"slug":"demo-pipeline","name":"Demo Pipeline","cluster_id":%s}`, clusterID)
				}))
				defer server.Close()
				args := []string{"--endpoint", server.URL, "pipeline", "rollback", "demo-pipeline", "--dry-run"}
				if jsonOutput {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer
				if err := Run(context.Background(), args, &stdout, &stderr, organizationClient(server.Client())); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(requests, []string{"GET /v2/organizations/acme/pipelines/demo-pipeline"}) {
					t.Fatalf("requests = %v", requests)
				}
				if stderr.Len() != 0 {
					t.Fatalf("stderr = %q", stderr.String())
				}
				if jsonOutput {
					var result struct {
						Pipeline string `json:"pipeline"`
						DryRun   bool   `json:"dry_run"`
						Note     string `json:"note"`
					}
					if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.Pipeline != "demo-pipeline" || !result.DryRun || !strings.Contains(result.Note, "not previewed") {
						t.Fatalf("result = %#v", result)
					}
				} else {
					want := `PIPELINE ROLLBACK (DRY RUN)
Pipeline: demo-pipeline

PROPOSED CHANGE

The pipeline would be unclustered and eligible clustered builds considered for cancellation, even if already unclustered.

CONTEXT

The server would use a fixed cutoff of rollback start minus 2 hours, with two bounded best-effort passes.
Targets are not previewed; rollback permissions are not checked. No changes made.
Cancellable states: creating, scheduled, started, failing, blocked. Already canceling builds count as pending; terminal builds (including failed) are excluded.
Queue routing percentages and concurrency-group migration state remain unchanged.
`
					if stdout.String() != want {
						t.Fatalf("output = %q, want %q", stdout.String(), want)
					}
					t.Logf("stdout:\n%s", &stdout)
				}
			})
		}
	}
}

func TestPipelineRollbackDryRunResolvesIdentifiers(t *testing.T) {
	for _, identifier := range []string{"Demo/Pipeline", testPipelineID} {
		t.Run(identifier, func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.EscapedPath())
				if r.Method != http.MethodGet {
					t.Errorf("dry run mutated: %s", r.Method)
				}
				switch r.URL.Path {
				case "/v2/organizations/acme/pipelines/" + identifier:
					w.WriteHeader(http.StatusNotFound)
					_, _ = fmt.Fprint(w, `{"message":"Not found"}`)
				case "/v2/organizations/acme/pipelines":
					_, _ = fmt.Fprintf(w, `[{"id":%q,"name":%q,"slug":"demo-pipeline"}]`, testPipelineID, identifier)
				case "/v2/organizations/acme/pipelines/demo-pipeline":
					_, _ = fmt.Fprint(w, `{"slug":"demo-pipeline","cluster_id":null}`)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()
			var stdout bytes.Buffer
			err := Run(context.Background(), []string{"--endpoint", server.URL, "pipeline", "rollback", identifier, "--dry-run"}, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
			if err != nil {
				t.Fatal(err)
			}
			want := []string{
				"GET /v2/organizations/acme/pipelines/" + url.PathEscape(identifier),
				"GET /v2/organizations/acme/pipelines",
				"GET /v2/organizations/acme/pipelines/demo-pipeline",
			}
			if !reflect.DeepEqual(requests, want) || !strings.Contains(stdout.String(), "Pipeline: demo-pipeline") {
				t.Fatalf("requests=%v output=%s", requests, &stdout)
			}
		})
	}
}

func TestPipelineRollbackErrorsDoNotPrintSuccessOrRetry(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		response string
		dryRun   bool
		want     string
	}{
		{"forbidden", 403, `{"message":"Build cancellation forbidden"}`, false, "Build cancellation forbidden"},
		{"validation", 422, `{"message":"Cannot clear assignment"}`, false, "Cannot clear assignment"},
		{"unavailable", 503, `{"message":"Disabled"}`, false, "Disabled"},
		{"server failure", 500, `{"message":"Internal error"}`, false, "may already have changed"},
		{"invalid result", 200, `{}`, false, "rollback response missing"},
		{"dry run forbidden", 403, `{"message":"Read forbidden"}`, true, "Read forbidden"},
		{"dry run invalid result", 200, `{}`, true, "pipeline response missing slug"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.response)
			}))
			defer server.Close()
			args := []string{"--endpoint", server.URL, "pipeline", "rollback", "demo-pipeline", "--json"}
			if test.dryRun {
				args = append(args, "--dry-run")
			}
			var stdout bytes.Buffer
			err := Run(context.Background(), args, &stdout, &bytes.Buffer{}, organizationClient(server.Client()))
			if err == nil || !strings.Contains(err.Error(), test.want) || requests != 1 || stdout.Len() != 0 {
				t.Fatalf("err=%v requests=%d stdout=%q", err, requests, stdout.String())
			}
		})
	}
}

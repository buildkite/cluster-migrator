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
	for _, test := range []struct {
		name     string
		response string
		wantText string
		wantErr  bool
	}{
		{"clear", testPipelineRollbackResponse, "Pipeline cluster assignment cleared.", false},
		{"already unclustered", strings.Replace(testPipelineRollbackResponse, `"assignment_changed":true`, `"assignment_changed":false`, 1), "Pipeline was already unclustered; residual cleanup attempted.", false},
		{"pending", strings.Replace(testPipelineRollbackResponse, `"pending":0`, `"pending":250`, 1), "Still-live clustered builds in window: 250", true},
		{"enqueued", strings.Replace(strings.Replace(testPipelineRollbackResponse, `"selected":0`, `"selected":1`, 1), `"cancellation_enqueued":0`, `"cancellation_enqueued":1`, 1), "Cancellation enqueued: 1", false},
		{"failure", strings.Replace(strings.Replace(testPipelineRollbackResponse, `"selected":0`, `"selected":1`, 1), `"failures":[]`, `"failures":[{"build_uuid":"build-uuid","message":"Enqueue failed"}]`, 1), "build-uuid: Enqueue failed", true},
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
				if (err != nil) != test.wantErr || (err != nil && !strings.Contains(err.Error(), "cleanup incomplete")) {
					t.Fatalf("error = %v, wantErr = %t", err, test.wantErr)
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
					for _, text := range []string{test.wantText, "PIPELINE ROLLBACK", "2026-09-08T01:00:00Z", "2026-09-08T03:00:01Z", "creating, scheduled, started, failing, blocked", "canceling", "best-effort", "2-hour", "does not mean jobs have stopped or concurrency slots are released", "Queue routing percentages and concurrency-group migration state remain unchanged"} {
						if !strings.Contains(stdout.String(), text) {
							t.Errorf("output missing %q: %s", text, &stdout)
						}
					}
					if test.wantErr && !strings.Contains(stdout.String(), "Inspect") {
						t.Errorf("missing next action: %s", &stdout)
					}
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
					for _, text := range []string{"PIPELINE ROLLBACK (DRY RUN)", "demo-pipeline", "would", "not previewed", "Queue routing percentages and concurrency-group migration state remain unchanged"} {
						if !strings.Contains(stdout.String(), text) {
							t.Errorf("output missing %q: %s", text, &stdout)
						}
					}
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

package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

const testPipelineID = "849411f9-9e6d-4739-a0d8-e247088e9b52"

const testPipelineReadinessResponse = `{"status":"no_known_blockers","destination_cluster_id":"parent-cluster","queue_observation":{"next_refresh_at":"2026-09-01T07:01:00Z"},"concurrency_group_observation":{"next_refresh_at":"2026-09-01T07:01:00Z"}}`

func TestPipelineCommandsInheritOrganizationMigrationDestination(t *testing.T) {
	for _, command := range []struct {
		name string
		args []string
	}{
		{name: "readiness", args: []string{"pipeline", "readiness", "demo-pipeline"}},
		{name: "move", args: []string{"pipeline", "move", "demo-pipeline"}},
		{name: "dry run", args: []string{"--dry-run", "pipeline", "move", "demo-pipeline"}},
	} {
		for _, test := range []struct {
			name      string
			response  string
			status    int
			wantError string
		}{
			{name: "parent destination differs from defaults"},
			{name: "missing migration", status: http.StatusNotFound, response: `{"message":"Organization migration is not configured"}`, wantError: "migration configure --destination-cluster <cluster>"},
			{name: "access denied", status: http.StatusForbidden, response: `{"message":"Forbidden"}`, wantError: "Buildkite API returned 403: Forbidden"},
			{name: "unresolvable destination", status: http.StatusNotFound, response: `{"message":"Destination cluster not found"}`, wantError: "Buildkite API returned 404: Destination cluster not found"},
		} {
			t.Run(command.name+"/"+test.name, func(t *testing.T) {
				t.Setenv("BUILDKITE_API_TOKEN", "secret")
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				operations, posts, discoveries := 0, 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method != http.MethodGet {
						posts++
					}
					switch r.URL.Path {
					case "/v2/organizations":
						discoveries++
						_, _ = fmt.Fprint(w, `[{"slug":"acme","default_cluster_id":"default-cluster"}]`)
					case "/v2/organizations/acme/unclustered-to-clustered/migration", "/v2/organizations/acme/clusters":
						t.Error("pipeline commands must not preflight migration status or clusters")
						w.WriteHeader(http.StatusForbidden)
						_, _ = fmt.Fprint(w, `{"message":"Forbidden"}`)
					case "/v2/organizations/acme/pipelines":
						_, _ = fmt.Fprint(w, `[{"name":"Demo Pipeline","slug":"demo-pipeline","cluster_id":"pipeline-cluster"}]`)
					case "/v2/organizations/acme/unclustered-to-clustered/migration/pipelines/demo-pipeline":
						operations++
						wantMethod := http.MethodGet
						if command.name == "move" {
							wantMethod = http.MethodPatch
						}
						if r.Method != wantMethod {
							t.Errorf("method = %q, want %q", r.Method, wantMethod)
						}
						body, err := io.ReadAll(r.Body)
						if err != nil || len(body) != 0 || r.URL.RawQuery != "" {
							t.Errorf("unexpected operation inputs: body = %q, query = %q, error = %v", body, r.URL.RawQuery, err)
						}
						if test.status != 0 {
							w.WriteHeader(test.status)
							_, _ = fmt.Fprint(w, test.response)
							return
						}
						if r.Method == http.MethodPatch {
							_, _ = fmt.Fprint(w, `{"slug":"demo-pipeline","cluster_id":"parent-cluster"}`)
						} else {
							_, _ = fmt.Fprint(w, strings.Replace(testPipelineReadinessResponse, `"status":`, `"pipeline":"demo-pipeline","status":`, 1))
						}
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				var stdout, stderr bytes.Buffer
				args := append([]string{"--endpoint", server.URL}, command.args...)
				err := Run(context.Background(), args, &stdout, &stderr, server.Client())
				if discoveries != 1 {
					t.Fatalf("token organization discoveries = %d, want 1", discoveries)
				}
				if test.wantError != "" {
					if err == nil || !strings.Contains(err.Error(), test.wantError) {
						t.Fatalf("error = %v, want %q", err, test.wantError)
					}
					if stdout.Len() != 0 {
						t.Fatalf("rejected operation stdout = %q", stdout.String())
					}
					if command.name != "move" && posts != 0 {
						t.Fatalf("read-only operation sent %d POSTs", posts)
					}
					if test.name == "missing migration" && operations != 1 {
						t.Fatalf("missing migration should not retry pipeline discovery: operations = %d", operations)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				wantPosts := 0
				if command.name == "move" {
					wantPosts = 1
				}
				if operations != 1 || posts != wantPosts {
					t.Fatalf("operations/posts = %d/%d, want 1/%d", operations, posts, wantPosts)
				}
				if !strings.Contains(stdout.String(), "Destination: parent-cluster\n") || stderr.Len() != 0 {
					t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
				}
			})
		}
	}
}

func TestPipelineCommandsAcceptSlugExactNameOrID(t *testing.T) {
	tests := []struct {
		name        string
		identifier  string
		method      string
		response    string
		status      int
		wantError   string
		run         func(*Context) error
		wantLookups int
		wantName    string
		wantRuns    int
	}{
		{
			name:       "readiness slug",
			identifier: "demo-pipeline",
			method:     http.MethodGet,
			response:   testPipelineReadinessResponse,
			run: func(app *Context) error {
				return (&PipelineReadinessCmd{Pipeline: "demo-pipeline"}).Run(app)
			},
			wantRuns: 1,
		},
		{
			name:       "readiness name",
			identifier: "Demo Pipeline",
			method:     http.MethodGet,
			response:   testPipelineReadinessResponse,
			run: func(app *Context) error {
				return (&PipelineReadinessCmd{Pipeline: "Demo Pipeline"}).Run(app)
			},
			wantLookups: 1,
			wantName:    "Demo Pipeline",
			wantRuns:    2,
		},
		{
			name:       "readiness ID",
			identifier: testPipelineID,
			method:     http.MethodGet,
			response:   testPipelineReadinessResponse,
			run: func(app *Context) error {
				return (&PipelineReadinessCmd{Pipeline: testPipelineID}).Run(app)
			},
			wantLookups: 1,
			wantRuns:    2,
		},
		{
			name:       "move ID",
			identifier: testPipelineID,
			method:     http.MethodPatch,
			response:   `{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","name":"Demo Pipeline","slug":"demo-pipeline","cluster_id":"cluster-id"}`,
			run: func(app *Context) error {
				return (&PipelineMoveCmd{Pipeline: testPipelineID}).Run(app)
			},
			wantLookups: 1,
			wantRuns:    2,
		},
		{
			name:       "missing migration after ID resolution",
			identifier: testPipelineID,
			method:     http.MethodPatch,
			response:   `{"message":"Organization migration is not configured"}`,
			status:     http.StatusNotFound,
			wantError:  "migration configure --destination-cluster <cluster>",
			run: func(app *Context) error {
				return (&PipelineMoveCmd{Pipeline: testPipelineID}).Run(app)
			},
			wantLookups: 1,
			wantRuns:    2,
		},
		{
			name:       "move slug",
			identifier: "demo-pipeline",
			method:     http.MethodPatch,
			response:   `{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","name":"Demo Pipeline","slug":"demo-pipeline","cluster_id":"cluster-id"}`,
			run: func(app *Context) error {
				return (&PipelineMoveCmd{Pipeline: "demo-pipeline"}).Run(app)
			},
			wantRuns: 1,
		},
		{
			name:       "move name with slash",
			identifier: "Demo/Pipeline",
			method:     http.MethodPatch,
			response:   `{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","name":"Demo/Pipeline","slug":"demo-pipeline","cluster_id":"cluster-id"}`,
			run: func(app *Context) error {
				return (&PipelineMoveCmd{Pipeline: "Demo/Pipeline"}).Run(app)
			},
			wantLookups: 1,
			wantName:    "Demo/Pipeline",
			wantRuns:    2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operationRuns := 0
			nameLookups := 0
			pipelineBasePath := "/v2/organizations/acme/unclustered-to-clustered/migration/pipelines/"
			directPath := pipelineBasePath + url.PathEscape(test.identifier)
			resolvedPath := pipelineBasePath + "demo-pipeline"

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/v2/organizations/acme/pipelines":
					nameLookups++
					if got := r.URL.Query().Get("name"); got != test.wantName {
						t.Fatalf("pipeline name query = %q, want %q", got, test.wantName)
					}
					_, _ = fmt.Fprintf(w, `[{"id":%q,"name":%q,"slug":"demo-pipeline"}]`, testPipelineID, test.identifier)
				case r.URL.EscapedPath() == directPath || r.URL.EscapedPath() == resolvedPath:
					operationRuns++
					if r.Method != test.method {
						t.Fatalf("method = %q, want %q", r.Method, test.method)
					}
					if test.identifier != "demo-pipeline" && operationRuns == 1 {
						w.WriteHeader(http.StatusNotFound)
						_, _ = w.Write([]byte(`{"message":"No pipeline found"}`))
						return
					}
					if test.status != 0 {
						w.WriteHeader(test.status)
					}
					_, _ = w.Write([]byte(test.response))
				default:
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
				}
			}))
			defer server.Close()

			client, err := buildkite.NewClient(server.URL, "acme", "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			app := Context{Context: context.Background(), Client: client, Output: &bytes.Buffer{}, ErrorOutput: &bytes.Buffer{}}
			err = test.run(&app)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want %q", err, test.wantError)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if nameLookups != test.wantLookups {
				t.Fatalf("name lookups = %d, want %d", nameLookups, test.wantLookups)
			}
			if operationRuns != test.wantRuns {
				t.Fatalf("operation runs = %d, want %d", operationRuns, test.wantRuns)
			}
		})
	}
}

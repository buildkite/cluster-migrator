package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

const testPipelineID = "849411f9-9e6d-4739-a0d8-e247088e9b52"

const testPipelineReadinessResponse = `{"status":"no_known_blockers","queue_observation":{"next_refresh_at":"2026-09-01T07:01:00Z"},"concurrency_group_observation":{"next_refresh_at":"2026-09-01T07:01:00Z"}}`

func TestPipelineCommandsAcceptSlugExactNameOrID(t *testing.T) {
	tests := []struct {
		name        string
		identifier  string
		method      string
		pathSuffix  string
		response    string
		run         func(*Context) error
		wantLookups int
		wantName    string
		wantRuns    int
	}{
		{
			name:       "readiness slug",
			identifier: "demo-pipeline",
			method:     http.MethodGet,
			pathSuffix: "/readiness",
			response:   testPipelineReadinessResponse,
			run: func(app *Context) error {
				return (&PipelineReadinessCmd{Pipeline: "demo-pipeline", DestinationCluster: "production"}).Run(app)
			},
			wantRuns: 1,
		},
		{
			name:       "readiness name",
			identifier: "Demo Pipeline",
			method:     http.MethodGet,
			pathSuffix: "/readiness",
			response:   testPipelineReadinessResponse,
			run: func(app *Context) error {
				return (&PipelineReadinessCmd{Pipeline: "Demo Pipeline", DestinationCluster: "production"}).Run(app)
			},
			wantLookups: 1,
			wantName:    "Demo Pipeline",
			wantRuns:    2,
		},
		{
			name:       "readiness ID",
			identifier: testPipelineID,
			method:     http.MethodGet,
			pathSuffix: "/readiness",
			response:   testPipelineReadinessResponse,
			run: func(app *Context) error {
				return (&PipelineReadinessCmd{Pipeline: testPipelineID, DestinationCluster: "production"}).Run(app)
			},
			wantLookups: 1,
			wantRuns:    2,
		},
		{
			name:       "move ID",
			identifier: testPipelineID,
			method:     http.MethodPost,
			pathSuffix: "/move",
			response:   `{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","name":"Demo Pipeline","slug":"demo-pipeline","cluster_id":"cluster-id"}`,
			run: func(app *Context) error {
				return (&PipelineMoveCmd{Pipeline: testPipelineID, DestinationCluster: "production"}).Run(app)
			},
			wantLookups: 1,
			wantRuns:    2,
		},
		{
			name:       "move slug",
			identifier: "demo-pipeline",
			method:     http.MethodPost,
			pathSuffix: "/move",
			response:   `{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","name":"Demo Pipeline","slug":"demo-pipeline","cluster_id":"cluster-id"}`,
			run: func(app *Context) error {
				return (&PipelineMoveCmd{Pipeline: "demo-pipeline", DestinationCluster: "production"}).Run(app)
			},
			wantRuns: 1,
		},
		{
			name:       "move name with slash",
			identifier: "Demo/Pipeline",
			method:     http.MethodPost,
			pathSuffix: "/move",
			response:   `{"id":"849411f9-9e6d-4739-a0d8-e247088e9b52","name":"Demo/Pipeline","slug":"demo-pipeline","cluster_id":"cluster-id"}`,
			run: func(app *Context) error {
				return (&PipelineMoveCmd{Pipeline: "Demo/Pipeline", DestinationCluster: "production"}).Run(app)
			},
			wantLookups: 1,
			wantName:    "Demo/Pipeline",
			wantRuns:    2,
		},
		{
			name:       "rollback slug",
			identifier: "demo-pipeline",
			method:     http.MethodPost,
			pathSuffix: "/rollback",
			response:   testPipelineRollbackResponse,
			run: func(app *Context) error {
				return (&PipelineRollbackCmd{Pipeline: "demo-pipeline"}).Run(app)
			},
			wantRuns: 1,
		},
		{
			name:       "rollback name with slash",
			identifier: "Demo/Pipeline",
			method:     http.MethodPost,
			pathSuffix: "/rollback",
			response:   testPipelineRollbackResponse,
			run: func(app *Context) error {
				return (&PipelineRollbackCmd{Pipeline: "Demo/Pipeline"}).Run(app)
			},
			wantLookups: 1,
			wantName:    "Demo/Pipeline",
			wantRuns:    2,
		},
		{
			name:       "rollback ID",
			identifier: testPipelineID,
			method:     http.MethodPost,
			pathSuffix: "/rollback",
			response:   testPipelineRollbackResponse,
			run: func(app *Context) error {
				return (&PipelineRollbackCmd{Pipeline: testPipelineID}).Run(app)
			},
			wantLookups: 1,
			wantRuns:    2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operationRuns := 0
			nameLookups := 0
			pipelineBasePath := "/v2/organizations/acme/cluster-queue-migrations/pipelines/"
			directPath := pipelineBasePath + url.PathEscape(test.identifier) + test.pathSuffix
			resolvedPath := pipelineBasePath + "demo-pipeline" + test.pathSuffix

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == "/v2/organizations/acme/clusters":
					_, _ = w.Write([]byte(`[{"id":"cluster-id","name":"production"}]`))
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
			if err := test.run(&app); err != nil {
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

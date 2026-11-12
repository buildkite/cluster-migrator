package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func organizationClient(client *http.Client) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v2/organizations" {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`[{"slug":"acme"}]`)),
				Request:    request,
			}, nil
		}
		return client.Transport.RoundTrip(request)
	})}
}

func TestCommandTimeoutReports(t *testing.T) {
	const base = "/v2/organizations/acme/"
	for _, test := range []struct {
		name, method, path, recovery string
		args                         []string
		known                        []string
	}{
		{name: "enrollment", args: []string{"migration", "configure", "--destination-cluster=production"}, method: "POST", path: base + "unclustered-to-clustered/migration", recovery: "migration status", known: []string{"Destination: cluster-id"}},
		{name: "prepare", args: []string{"migration", "set-phase", "prepare-queues"}, method: "POST", path: base + "unclustered-to-clustered/migration/prepare-queues", recovery: "migration status"},
		{name: "validate", args: []string{"migration", "set-phase", "validate-queues"}, method: "POST", path: base + "unclustered-to-clustered/migration/validate-queues", recovery: "migration status"},
		{name: "commit", args: []string{"migration", "set-phase", "cutover-concurrency-groups", "--acknowledge-irreversible"}, method: "POST", path: base + "unclustered-to-clustered/migration/cutover-concurrency-groups", recovery: "migration status"},
		{name: "pipeline phase", args: []string{"migration", "set-phase", "migrate-pipelines"}, method: "POST", path: base + "unclustered-to-clustered/migration/cutover-pipelines", recovery: "migration status"},
		{name: "complete", args: []string{"migration", "set-phase", "complete"}, method: "POST", path: base + "unclustered-to-clustered/migration/complete", recovery: "migration status"},
		{name: "finalize", args: []string{"finalize-blocked-builds"}, method: "POST", path: base + "unclustered-to-clustered/migration/finalizations", recovery: "migration status"},
		{name: "queue configure", args: []string{"queue", "configure", "deploy"}, method: "POST", path: base + "unclustered-to-clustered/queue-migrations", recovery: "queue status deploy", known: []string{"Migration phase: prepare_queues", "Destination: cluster-id"}},
		{name: "queue routing", args: []string{"queue", "set-percent", "deploy", "--to=60"}, method: "PATCH", path: base + "unclustered-to-clustered/queue-migrations/deploy", recovery: "queue status deploy", known: []string{"Routing: 30%", "Migration phase: prepare_queues"}},
		{name: "group cutover", args: []string{"concurrency-group", "cutover", "custom/deploy"}, method: "POST", path: base + "unclustered-to-clustered/concurrency-group-migrations/custom/deploy/cutover", recovery: "concurrency-group status custom/deploy", known: []string{"Destination: cluster-id"}},
		{name: "pipeline move", args: []string{"pipeline", "move", "monorepo"}, method: "PATCH", path: base + "unclustered-to-clustered/migration/pipelines/monorepo", recovery: "pipeline readiness monorepo"},
		{name: "quoted pipeline", args: []string{"pipeline", "move", "release job's $HOME"}, method: "PATCH", path: base + "unclustered-to-clustered/migration/pipelines/release job's $HOME", recovery: `pipeline readiness 'release job'\''s $HOME'`},
		{name: "migration status", args: []string{"migration", "status"}, method: "GET", path: base + "unclustered-to-clustered/migration", recovery: "migration status"},
		{name: "completion readiness", args: []string{"migration", "status"}, method: "GET", path: base + "unclustered-to-clustered/migration/readiness", recovery: "migration status", known: []string{"Migration phase: migrate_pipelines"}},
		{name: "queue status", args: []string{"queue", "status", "deploy"}, method: "GET", path: base + "unclustered-to-clustered/queue-migrations/deploy", recovery: "queue status deploy"},
		{name: "queue list", args: []string{"queue", "status"}, method: "GET", path: base + "unclustered-to-clustered/queue-migrations", recovery: "queue status"},
		{name: "metrics", args: []string{"queue", "metrics", "deploy"}, method: "GET", path: base + "unclustered-to-clustered/queue-migrations/deploy/metrics", recovery: "queue metrics deploy"},
		{name: "group status", args: []string{"concurrency-group", "status", "custom/deploy"}, method: "GET", path: base + "unclustered-to-clustered/concurrency-group-migrations/custom/deploy", recovery: "concurrency-group status custom/deploy"},
		{name: "group list", args: []string{"concurrency-group", "status"}, method: "GET", path: base + "unclustered-to-clustered/concurrency-group-migrations", recovery: "concurrency-group status"},
		{name: "pipeline readiness", args: []string{"pipeline", "readiness", "monorepo"}, method: "GET", path: base + "unclustered-to-clustered/migration/pipelines/monorepo", recovery: "pipeline readiness monorepo"},
		{name: "preflight", args: []string{"queue", "set-percent", "deploy", "--to=60"}, method: "GET", path: base + "unclustered-to-clustered/migration", recovery: "queue status deploy"},
		{name: "discovery", args: []string{"pipeline", "move", "monorepo"}, method: "GET", path: "/v2/organizations", recovery: "pipeline readiness monorepo"},
		{name: "pipeline fallback", args: []string{"pipeline", "move", "monorepo"}, method: "GET", path: base + "pipelines", recovery: "pipeline readiness monorepo"},
	} {
		for _, jsonOutput := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", test.name, jsonOutput), func(t *testing.T) {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				mutations, failures := 0, 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if failures > 0 {
						t.Errorf("unexpected recovery request after timeout: %s %s", r.Method, r.URL)
					}
					if r.Method != http.MethodGet {
						mutations++
					}
					if r.Method == test.method && r.URL.Path == test.path {
						failures++
						w.WriteHeader(http.StatusGatewayTimeout)
						return
					}
					var body string
					switch r.URL.Path {
					case "/v2/organizations":
						body = `[{"slug":"acme"}]`
					case base + "clusters":
						body = `[{"id":"cluster-id","name":"production"}]`
					case base + "clusters/cluster-id/queues":
						body = `[{"id":"queue-id","key":"deploy"}]`
					case base + "unclustered-to-clustered/migration":
						phase := "prepare_queues"
						switch test.name {
						case "completion readiness":
							phase = "migrate_pipelines"
						case "group cutover":
							phase = "cutover_concurrency_groups"
						}
						body = fmt.Sprintf(`{"destination_cluster_id":"cluster-id","phase":%q}`, phase)
					case base + "unclustered-to-clustered/queue-migrations/deploy":
						body = `{"queue_key":"deploy","routed_percent":30,"destination":{"cluster_id":"cluster-id"}}`
					case base + "unclustered-to-clustered/migration/pipelines/monorepo":
						w.WriteHeader(http.StatusNotFound)
						body = `{"message":"Pipeline not found"}`
					default:
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
						w.WriteHeader(http.StatusNotFound)
						return
					}
					_, _ = fmt.Fprint(w, body)
				}))
				defer server.Close()
				var stdout, stderr bytes.Buffer
				args := append([]string{"--api-token=private-test-token", "--endpoint", server.URL}, test.args...)
				if jsonOutput {
					args = append([]string{"--api-token", "private-test-token", "--endpoint", server.URL, "--json"}, test.args...)
				}
				err := Run(context.Background(), args, &stdout, &stderr, server.Client())
				if err == nil || stdout.Len() != 0 || stderr.Len() != 0 || failures != 1 {
					t.Fatalf("error=%v stdout=%q stderr=%q failures=%d; want error, clean output and one failed request", err, &stdout, &stderr, failures)
				}
				for _, text := range append([]string{"COMMAND\n", "RESULT\n", "LAST KNOWN INFORMATION", "NEXT STEPS\n", "cluster-migrator " + test.recovery}, test.known...) {
					if !strings.Contains(err.Error(), text) {
						t.Errorf("missing %q in %v", text, err)
					}
				}
				if len(test.known) > 0 && !strings.Contains(err.Error(), "not confirmed current state") {
					t.Errorf("observations not labeled as last known: %v", err)
				}
				wantMutations := 0
				if test.method != http.MethodGet {
					wantMutations = 1
					if !strings.Contains(err.Error(), "Mutation outcome is unknown") {
						t.Errorf("missing uncertain-outcome warning: %v", err)
					}
				} else if test.name == "pipeline fallback" {
					wantMutations = 1
					if strings.Contains(err.Error(), "No mutation was attempted") || strings.Contains(err.Error(), "Mutation outcome is unknown") {
						t.Errorf("rejected PATCH followed by read timeout misrepresented: %v", err)
					}
				} else if !strings.Contains(err.Error(), "No mutation was attempted") || strings.Contains(err.Error(), "Mutation outcome is unknown") {
					t.Errorf("read failure misrepresented: %v", err)
				}
				if mutations != wantMutations || strings.Contains(err.Error(), "private-test-token") || strings.Contains(err.Error(), "--api-token") {
					t.Errorf("mutations=%d, want %d, or credentials exposed: %v", mutations, wantMutations, err)
				}
			})
		}
	}
}

func TestReadRequestTimeoutReports(t *testing.T) {
	for _, args := range [][]string{{"queue", "metrics", "deploy"}, {"pipeline", "readiness", "monorepo"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", "secret")
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			requests := 0
			client := organizationClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.Method != http.MethodGet {
					t.Errorf("unexpected mutation: %s %s", request.Method, request.URL)
				}
				return nil, context.DeadlineExceeded
			})})
			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), append(args, "--json"), &stdout, &stderr, client)
			if !errors.Is(err, context.DeadlineExceeded) || requests != 1 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("error=%v requests=%d stdout=%q stderr=%q", err, requests, &stdout, &stderr)
			}
			if !strings.Contains(err.Error(), "No mutation was attempted") || !strings.Contains(err.Error(), "cluster-migrator "+strings.Join(args, " ")) {
				t.Errorf("missing read timeout report: %v", err)
			}
		})
	}
}

func TestHelpDocumentsAPITokenWithoutRequiringItInUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"queue"}, {"queue", "status"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var root rootCommand
			var output bytes.Buffer
			parser, err := kong.New(
				&root,
				kong.Name("cluster-migrator"),
				kong.Writers(&output, &output),
			)
			if err != nil {
				t.Fatal(err)
			}
			context, err := kong.Trace(parser, args)
			if err != nil {
				t.Fatal(err)
			}
			if err := context.PrintUsage(false); err != nil {
				t.Fatal(err)
			}
			help := output.String()
			if got := strings.Count(help, "--api-token"); got != 1 {
				t.Fatalf("help contains --api-token %d times, want only the extended flag documentation:\n%s", got, help)
			}
			if !strings.Contains(help, "--api-token=STRING") || !strings.Contains(help, "($BUILDKITE_API_TOKEN)") {
				t.Fatalf("extended help does not document --api-token and its environment variable:\n%s", help)
			}
		})
	}
}

func TestQueueStatusShortUsageDoesNotRequireAPIToken(t *testing.T) {
	var root rootCommand
	var output bytes.Buffer
	parser, err := kong.New(
		&root,
		kong.Name("cluster-migrator"),
		kong.Writers(&output, &output),
	)
	if err != nil {
		t.Fatal(err)
	}
	context, err := kong.Trace(parser, []string{"queue", "status"})
	if err != nil {
		t.Fatal(err)
	}
	if err := context.PrintUsage(true); err != nil {
		t.Fatal(err)
	}
	if got := strings.SplitN(output.String(), "\n", 2)[0]; got != "Usage: cluster-migrator queue status [<queue>]" {
		t.Fatalf("usage = %q", got)
	}
}

func TestRunAcceptsAPITokenFromEnvironmentAndFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		envToken  string
		wantToken string
	}{
		{name: "environment", envToken: "environment-token", wantToken: "environment-token"},
		{name: "flag takes precedence", args: []string{"--api-token", "flag-token"}, envToken: "environment-token", wantToken: "flag-token"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("BUILDKITE_API_TOKEN", test.envToken)
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if got, want := r.Header.Get("Authorization"), "Bearer "+test.wantToken; got != want {
					t.Errorf("Authorization = %q, want %q", got, want)
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v2/organizations":
					_, _ = w.Write([]byte(`[{"slug":"acme"}]`))
				case "/v2/organizations/acme/clusters":
					_, _ = w.Write([]byte(`[]`))
				case "/v2/organizations/acme/unclustered-to-clustered/queue-migrations":
					_, _ = w.Write([]byte(`{"items":[],"links":{}}`))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			args := append([]string{"--endpoint", server.URL}, test.args...)
			args = append(args, "queue", "status")
			if err := Run(context.Background(), args, &bytes.Buffer{}, &bytes.Buffer{}, server.Client()); err != nil {
				t.Fatal(err)
			}
			if requests != 3 {
				t.Fatalf("requests = %d, want 3", requests)
			}
		})
	}
}

func TestRunRequiresAPITokenFromEnvironmentOrFlag(t *testing.T) {
	t.Setenv("BUILDKITE_API_TOKEN", "")

	err := Run(context.Background(), []string{"queue", "status"}, &bytes.Buffer{}, &bytes.Buffer{}, nil)
	if err == nil || err.Error() != "--api-token or BUILDKITE_API_TOKEN is required" {
		t.Fatalf("error = %v", err)
	}
}

func TestPipelineUsageAcceptsIDNameOrSlug(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "root"},
		{name: "pipeline", args: []string{"pipeline"}},
		{name: "readiness", args: []string{"pipeline", "readiness"}},
		{name: "move", args: []string{"pipeline", "move"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var root rootCommand
			var output bytes.Buffer
			parser, err := kong.New(
				&root,
				kong.Name("cluster-migrator"),
				kong.Writers(&output, &output),
			)
			if err != nil {
				t.Fatal(err)
			}
			context, err := kong.Trace(parser, test.args)
			if err != nil {
				t.Fatal(err)
			}
			if err := context.PrintUsage(false); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "<pipeline>") {
				t.Fatalf("usage does not identify pipeline argument:\n%s", output.String())
			}
			if strings.Contains(output.String(), "<pipeline-slug>") {
				t.Fatalf("usage requires a pipeline slug:\n%s", output.String())
			}
			if test.name == "readiness" || test.name == "move" {
				if !strings.Contains(output.String(), "Pipeline ID, name, or slug.") {
					t.Fatalf("help does not describe accepted pipeline identifiers:\n%s", output.String())
				}
				if strings.Contains(output.String(), "--destination-cluster") {
					t.Fatalf("help includes removed destination flag:\n%s", output.String())
				}
			}
			if test.name == "move" && strings.Contains(output.String(), "--wait") {
				t.Fatalf("move help includes removed --wait option:\n%s", output.String())
			}
		})
	}
}

func TestPipelineDestinationOverrideFailsBeforeRequests(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		t.Errorf("removed flag must fail before API requests: %s %s", request.Method, request.URL)
		return nil, fmt.Errorf("unexpected API request")
	})}
	for _, args := range [][]string{
		{"pipeline", "readiness", "monorepo", "--destination-cluster", "production"},
		{"pipeline", "move", "monorepo", "--destination-cluster", "production"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), args, &stdout, &stderr, client)
			if err == nil || !strings.Contains(err.Error(), "unknown flag --destination-cluster") || stdout.Len() != 0 {
				t.Fatalf("error = %v, stdout = %q", err, stdout.String())
			}
		})
	}
}

func TestEndpointDefaultsToRESTAPI(t *testing.T) {
	var root rootCommand
	parser, err := kong.New(&root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse([]string{
		"--api-token", "secret",
		"queue", "status",
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := root.Endpoint, "https://api.buildkite.com/"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

func TestRemovedDryRunRejectsBeforeRequests(t *testing.T) {
	commands := [][]string{
		{"queue", "configure", "deploy"},
		{"queue", "set-percent", "deploy", "--to=50"},
		{"queue", "status"}, {"queue", "status", "deploy"}, {"queue", "metrics", "deploy"},
		{"concurrency-group", "cutover", "custom/deploy"},
		{"concurrency-group", "status"}, {"concurrency-group", "status", "custom/deploy"},
		{"pipeline", "move", "demo"}, {"pipeline", "readiness", "demo"},
		{"migration", "configure", "--destination-cluster=production"}, {"migration", "status"},
		{"migration", "set-phase", "prepare-queues"}, {"migration", "set-phase", "validate-queues"},
		{"migration", "set-phase", "cutover-concurrency-groups", "--acknowledge-irreversible"},
		{"migration", "set-phase", "migrate-pipelines"}, {"migration", "set-phase", "complete"},
		{"finalize-blocked-builds"},
	}
	for _, command := range commands {
		for position := 0; position <= len(command); position++ {
			for _, jsonOutput := range []bool{false, true} {
				for _, token := range []string{"", "local-test-token"} {
					t.Run(fmt.Sprintf("%v/position=%d/json=%t/token=%t", command, position, jsonOutput, token != ""), func(t *testing.T) {
						t.Setenv("BUILDKITE_API_TOKEN", token)
						requests := 0
						client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
							requests++
							return nil, errors.New("unexpected HTTP request")
						})}
						args := append([]string{}, command[:position]...)
						args = append(args, "--dry-run")
						args = append(args, command[position:]...)
						if jsonOutput {
							args = append([]string{"--json"}, args...)
						}
						var stdout, stderr bytes.Buffer
						err := Run(context.Background(), args, &stdout, &stderr, client)
						if err == nil || !strings.Contains(err.Error(), "unknown flag --dry-run") || requests != 0 || stdout.Len() != 0 {
							t.Fatalf("error=%v requests=%d stdout=%q", err, requests, &stdout)
						}
					})
				}
			}
		}
	}
}

func TestVersionFlag(t *testing.T) {
	var root rootCommand
	var output bytes.Buffer
	exited := false
	defer func() {
		if recovered := recover(); recovered != "exit" {
			t.Fatalf("unexpected panic: %v", recovered)
		}
		if !exited {
			t.Fatal("version flag did not exit")
		}
		if got, want := output.String(), "cluster-migrator v1.2.3\n"; got != want {
			t.Fatalf("output = %q, want %q", got, want)
		}
	}()
	parser, err := kong.New(
		&root,
		kong.Vars{"version": "cluster-migrator v1.2.3"},
		kong.Writers(&output, &output),
		kong.Exit(func(status int) {
			if status != 0 {
				t.Fatalf("exit status = %d, want 0", status)
			}
			exited = true
			panic("exit")
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse([]string{"--version"}); err != nil {
		t.Fatal(err)
	}
	t.Fatal("version flag did not exit")
}

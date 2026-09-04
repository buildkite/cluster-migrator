package cli

import (
	"bytes"
	"context"
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
				case "/v2/organizations/acme/cluster-queue-migrations":
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
				if !strings.Contains(output.String(), "Destination cluster name or ID.") {
					t.Fatalf("help does not describe accepted cluster identifiers:\n%s", output.String())
				}
			}
			if test.name == "move" && strings.Contains(output.String(), "--wait") {
				t.Fatalf("move help includes removed --wait option:\n%s", output.String())
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

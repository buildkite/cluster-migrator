package cli

import (
	"bytes"
	"io"
	"net/http"
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

func TestQueueHelpIncludesAPIToken(t *testing.T) {
	var root rootCommand
	var output bytes.Buffer
	parser, err := kong.New(&root, kong.Writers(&output, &output))
	if err != nil {
		t.Fatal(err)
	}
	context, err := kong.Trace(parser, []string{"queue"})
	if err != nil {
		t.Fatal(err)
	}
	if err := context.PrintUsage(false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "--api-token=STRING") {
		t.Fatalf("queue help does not include --api-token:\n%s", output.String())
	}
}

func TestPipelineUsageIdentifiesPipelineSlug(t *testing.T) {
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
			if !strings.Contains(output.String(), "<pipeline-slug>") {
				t.Fatalf("usage does not identify pipeline slug:\n%s", output.String())
			}
			if strings.Contains(output.String(), "<pipeline>") {
				t.Fatalf("usage contains ambiguous pipeline argument:\n%s", output.String())
			}
			if test.name == "readiness" || test.name == "move" {
				if !strings.Contains(output.String(), "Pipeline slug (not pipeline name).") {
					t.Fatalf("help does not distinguish pipeline slug from name:\n%s", output.String())
				}
				if !strings.Contains(output.String(), "Destination cluster name or ID.") {
					t.Fatalf("help does not describe accepted cluster identifiers:\n%s", output.String())
				}
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

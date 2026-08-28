package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

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

func TestEndpointDefaultsToRESTAPI(t *testing.T) {
	var root rootCommand
	parser, err := kong.New(&root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.Parse([]string{
		"--organization", "acme",
		"--api-token", "secret",
		"queue", "status",
	}); err != nil {
		t.Fatal(err)
	}
	if got, want := root.Endpoint, "https://api.buildkite.com/"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}
}

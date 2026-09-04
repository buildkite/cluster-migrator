package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/alecthomas/kong"
	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

var version = "dev"

type rootCommand struct {
	Version  kong.VersionFlag `help:"Print version information and quit."`
	APIToken string           `help:"Buildkite API token." env:"BUILDKITE_API_TOKEN"`
	Endpoint string           `help:"Buildkite REST API endpoint." env:"BUILDKITE_API_ENDPOINT" default:"https://api.buildkite.com/"`
	JSON     bool             `help:"Write machine-readable JSON." global:""`
	DryRun   bool             `help:"Validate and display a mutation without applying it." global:""`
	Timeout  time.Duration    `help:"Maximum command duration." default:"10m" global:""`

	Queue    QueueCmd    `cmd:"" help:"Configure and inspect queue migrations."`
	Pipeline PipelineCmd `cmd:"" help:"Check and move pipelines."`
}

func Run(
	ctx context.Context,
	args []string,
	stdout, stderr io.Writer,
	httpClient *http.Client,
) error {
	var root rootCommand
	parser, err := kong.New(
		&root,
		kong.Name("cluster-migrator"),
		kong.Description("Safely migrate Buildkite workloads from unclustered queues to clusters."),
		kong.Vars{"version": "cluster-migrator " + version},
		kong.Writers(stdout, stderr),
	)
	if err != nil {
		return fmt.Errorf("create command parser: %w", err)
	}

	parsed, err := parser.Parse(args)
	if err != nil {
		var parseError *kong.ParseError
		if errors.As(err, &parseError) {
			parseError.Context.Stdout = stderr
			_ = parseError.Context.PrintUsage(false)
			_, _ = fmt.Fprintln(stderr)
		}
		return err
	}
	if root.APIToken == "" {
		return fmt.Errorf("--api-token or BUILDKITE_API_TOKEN is required")
	}

	commandContext, cancel := context.WithTimeout(ctx, root.Timeout)
	defer cancel()
	client, err := buildkite.NewClient(root.Endpoint, "", root.APIToken, httpClient)
	if err != nil {
		return err
	}
	if err := client.ResolveOrganization(commandContext); err != nil {
		return err
	}

	app := &Context{
		Context:     commandContext,
		Client:      client,
		Output:      stdout,
		ErrorOutput: stderr,
		JSON:        root.JSON,
		DryRun:      root.DryRun,
		Now:         time.Now,
	}
	return parsed.Run(app)
}

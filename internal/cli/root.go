package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/alecthomas/kong"
	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type rootCommand struct {
	Organization string        `help:"Buildkite organization slug." env:"BUILDKITE_ORGANIZATION_SLUG" required:""`
	APIToken     string        `help:"Buildkite API token." env:"BUILDKITE_API_TOKEN" required:"" hidden:""`
	Endpoint     string        `help:"Buildkite REST API endpoint." env:"BUILDKITE_API_ENDPOINT" default:"https://api.buildkite.com/"`
	JSON         bool          `help:"Write machine-readable JSON." global:""`
	DryRun       bool          `help:"Validate and display a mutation without applying it." global:""`
	Yes          bool          `help:"Skip interactive confirmation." global:""`
	Timeout      time.Duration `help:"Maximum command duration." default:"10m" global:""`

	Queue            QueueCmd            `cmd:"" help:"Configure and inspect queue migrations."`
	ConcurrencyGroup ConcurrencyGroupCmd `cmd:"" help:"Cut over and inspect concurrency groups."`
	Pipeline         PipelineCmd         `cmd:"" help:"Check and move pipelines."`
}

func Run(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	httpClient *http.Client,
) error {
	var root rootCommand
	parser, err := kong.New(
		&root,
		kong.Name("cluster-migrator"),
		kong.Description("Safely migrate Buildkite workloads from unclustered queues to clusters."),
		kong.Writers(stdout, stderr),
	)
	if err != nil {
		return fmt.Errorf("create command parser: %w", err)
	}

	parsed, err := parser.Parse(args)
	if err != nil {
		return err
	}

	client, err := buildkite.NewClient(root.Endpoint, root.Organization, root.APIToken, httpClient)
	if err != nil {
		return err
	}

	commandContext, cancel := context.WithTimeout(ctx, root.Timeout)
	defer cancel()

	app := &Context{
		Context:     commandContext,
		Client:      client,
		Input:       stdin,
		Output:      stdout,
		ErrorOutput: stderr,
		JSON:        root.JSON,
		DryRun:      root.DryRun,
		Yes:         root.Yes,
	}
	return parsed.Run(app)
}

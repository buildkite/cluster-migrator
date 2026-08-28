package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type Context struct {
	Context context.Context
	Client  *buildkite.Client
	Output  io.Writer
	JSON    bool
	DryRun  bool
}

type Change struct {
	Action             string `json:"action"`
	Resource           string `json:"resource"`
	DestinationCluster string `json:"destination_cluster,omitempty"`
	FromPercent        *int   `json:"from_percent,omitempty"`
	ToPercent          *int   `json:"to_percent,omitempty"`
	DryRun             bool   `json:"dry_run"`
}

func (c *Context) Print(value any) error {
	if c.JSON {
		encoder := json.NewEncoder(c.Output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}

	switch value := value.(type) {
	case *buildkite.QueueMigration:
		_, err := fmt.Fprintf(c.Output, "%s\t%d%%\t%s\n", value.QueueKey, value.RoutedPercent, value.Destination.ClusterID)
		return err
	case []buildkite.QueueMigration:
		for i := range value {
			if err := c.Print(&value[i]); err != nil {
				return err
			}
		}
		return nil
	case Change:
		var line strings.Builder
		_, _ = fmt.Fprintf(&line, "%s %s", value.Action, value.Resource)
		if value.FromPercent != nil && value.ToPercent != nil {
			_, _ = fmt.Fprintf(&line, ": %d%% -> %d%%", *value.FromPercent, *value.ToPercent)
		} else if value.ToPercent != nil {
			_, _ = fmt.Fprintf(&line, " at %d%%", *value.ToPercent)
		}
		if value.DestinationCluster != "" {
			_, _ = fmt.Fprintf(&line, " in %s", value.DestinationCluster)
		}
		if value.DryRun {
			_, _ = fmt.Fprint(&line, " (dry run)")
		}
		_, err := fmt.Fprintln(c.Output, line.String())
		return err
	default:
		encoder := json.NewEncoder(c.Output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
}

func (c *Context) Poll(check func() (bool, error)) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		done, err := check()
		if err != nil || done {
			return err
		}
		select {
		case <-c.Context.Done():
			return c.Context.Err()
		case <-ticker.C:
		}
	}
}

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
	Context     context.Context
	Client      *buildkite.Client
	Output      io.Writer
	ErrorOutput io.Writer
	JSON        bool
	DryRun      bool
	Now         func() time.Time
	Wait        func(context.Context, time.Duration) error
}

type Change struct {
	Action             string `json:"action"`
	Resource           string `json:"resource"`
	DestinationCluster string `json:"destination_cluster,omitempty"`
	FromPercent        *int   `json:"from_percent,omitempty"`
	ToPercent          *int   `json:"to_percent,omitempty"`
	DryRun             bool   `json:"dry_run"`
}

type QueueDestination struct {
	ClusterID   string `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
}

type QueueChange struct {
	Command     string           `json:"-"`
	Queue       string           `json:"queue"`
	FromPercent *int             `json:"from_percent,omitempty"`
	ToPercent   int              `json:"to_percent"`
	Destination QueueDestination `json:"destination"`
	DryRun      bool             `json:"dry_run"`
	Next        string           `json:"-"`
	Note        string           `json:"-"`
}

type QueueStatus struct {
	Queue          string           `json:"queue"`
	RoutingPercent int              `json:"routing_percent"`
	Destination    QueueDestination `json:"destination"`
	Next           string           `json:"-"`
}

type PipelineMoveResult struct {
	Pipeline    *buildkite.Pipeline
	Destination string
}

func (c *Context) Print(value any) error {
	if c.JSON {
		if result, ok := value.(PipelineMoveResult); ok {
			value = result.Pipeline
		}
		encoder := json.NewEncoder(c.Output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}

	switch value := value.(type) {
	case QueueChange:
		heading := "RESULT"
		if value.DryRun {
			heading += " (dry run)"
		}
		if value.Command == "CONFIGURE" {
			result := fmt.Sprintf("A migration for the '%s' queue was successfully created in the cluster '%s'", value.Queue, value.Destination.ClusterName)
			if value.DryRun {
				result = fmt.Sprintf("A migration for the '%s' queue would be created in the cluster '%s'", value.Queue, value.Destination.ClusterName)
			}
			if _, err := fmt.Fprintf(c.Output, "%s\n\n%s\n", heading, result); err != nil {
				return err
			}
		} else {
			if _, err := fmt.Fprintf(c.Output, "QUEUE %s\nQueue: %s\nDestination: %s\n\n", value.Command, value.Queue, value.Destination.ClusterName); err != nil {
				return err
			}
			from := "—"
			if value.FromPercent != nil {
				from = fmt.Sprintf("%d%%", *value.FromPercent)
			}
			if _, err := fmt.Fprintf(c.Output, "%s\n\nRouting: %s → %d%%\n", heading, from, value.ToPercent); err != nil {
				return err
			}
		}
		if value.Next != "" {
			_, err := fmt.Fprintf(c.Output, "\nNEXT STEPS\n\n%s\n", value.Next)
			return err
		}
		if value.Note != "" {
			_, err := fmt.Fprintf(c.Output, "\nSOURCE\n\n%s\n", value.Note)
			return err
		}
		return nil
	case QueueStatus:
		if err := c.printQueueStatuses([]QueueStatus{value}); err != nil {
			return err
		}
		if value.Next != "" {
			_, err := fmt.Fprintf(c.Output, "\nNEXT STEPS\n\n%s\n", value.Next)
			return err
		}
		return nil
	case []QueueStatus:
		return c.printQueueStatuses(value)
	case *buildkite.QueueMetrics:
		return c.printQueueMetrics(value)
	case *buildkite.PipelineReadiness:
		return c.printPipelineReadiness(value)
	case PipelineMoveResult:
		pipeline := value.Pipeline.Name
		if pipeline == "" {
			pipeline = value.Pipeline.Slug
		} else {
			pipeline = fmt.Sprintf("%s (%s)", pipeline, value.Pipeline.Slug)
		}
		_, err := fmt.Fprintf(c.Output, "RESULT\n\n%s is now using the %s cluster\n", pipeline, value.Destination)
		return err
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

func (c *Context) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Context) wait(ctx context.Context, duration time.Duration) error {
	if c.Wait != nil {
		return c.Wait(ctx, duration)
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Context) printQueueStatuses(statuses []QueueStatus) error {
	if len(statuses) == 0 {
		_, err := fmt.Fprint(c.Output, "QUEUE STATUS\nMigrations: 0\n\nRESULT\n\nNo queue migrations configured.\n\nNEXT STEPS\n\n1. Configure a queue migration:\n\n   cluster-migrator queue configure <queue> --destination-cluster <cluster>\n")
		return err
	}
	rows := make([][]string, 0, len(statuses))
	for _, status := range statuses {
		rows = append(rows, []string{status.Queue, fmt.Sprintf("%d%%", status.RoutingPercent), status.Destination.ClusterName})
	}
	table := renderASCIITable(
		[]string{"Queue", "Routing", "Destination"},
		rows,
		[]bool{false, true, false},
	)
	_, err := fmt.Fprintf(c.Output, "QUEUE STATUS\n\n%s\n", strings.Join(table, "\n"))
	return err
}

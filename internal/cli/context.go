package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
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

func (c *Context) Print(value any) error {
	if c.JSON {
		encoder := json.NewEncoder(c.Output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}

	switch value := value.(type) {
	case QueueChange:
		if _, err := fmt.Fprintf(c.Output, "QUEUE %s\nQueue: %s\nDestination: %s\n\n", value.Command, value.Queue, value.Destination.ClusterName); err != nil {
			return err
		}
		heading := "RESULT"
		if value.DryRun {
			heading += " (dry run)"
		}
		from := "—"
		if value.FromPercent != nil {
			from = fmt.Sprintf("%d%%", *value.FromPercent)
		}
		if _, err := fmt.Fprintf(c.Output, "%s\n\nRouting: %s → %d%%\n", heading, from, value.ToPercent); err != nil {
			return err
		}
		if value.Next != "" {
			_, err := fmt.Fprintf(c.Output, "\nNEXT\n\n%s\n", value.Next)
			return err
		}
		if value.Note != "" {
			_, err := fmt.Fprintf(c.Output, "\nSOURCE\n\n%s\n", value.Note)
			return err
		}
		return nil
	case QueueStatus:
		if _, err := fmt.Fprintf(c.Output, "QUEUE STATUS\nQueue: %s\nDestination: %s\n\nRESULT\n\nRouting: %d%%\n", value.Queue, value.Destination.ClusterName, value.RoutingPercent); err != nil {
			return err
		}
		if value.Next != "" {
			_, err := fmt.Fprintf(c.Output, "\nNEXT\n\n%s\n", value.Next)
			return err
		}
		return nil
	case []QueueStatus:
		return c.printQueueStatuses(value)
	case *buildkite.QueueMetrics:
		return c.printQueueMetrics(value)
	case *buildkite.PipelineReadiness:
		return c.printPipelineReadiness(value)
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
		_, err := fmt.Fprint(c.Output, "QUEUE STATUS\nMigrations: 0\n\nRESULT\n\nNo queue migrations configured.\n\nNEXT\n\nConfigure a queue migration:\n\n  cluster-migrator queue configure <queue> --destination-cluster <cluster>\n")
		return err
	}
	if _, err := fmt.Fprintf(c.Output, "QUEUE STATUS\nMigrations: %d\n\nRESULT\n\n", len(statuses)); err != nil {
		return err
	}

	writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "QUEUE\tROUTING\tDESTINATION")
	for _, status := range statuses {
		_, _ = fmt.Fprintf(writer, "%s\t%d%%\t%s\n", status.Queue, status.RoutingPercent, status.Destination.ClusterName)
	}
	return writer.Flush()
}

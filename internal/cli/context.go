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
		heading := "RESULT"
		if value.DryRun {
			heading = "PROPOSED CHANGE"
		}
		if _, err := fmt.Fprintf(c.Output, "%s\n\n", heading); err != nil {
			return err
		}
		writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(writer, "QUEUE\tFROM\tTO\tDESTINATION")
		from := "—"
		if value.FromPercent != nil {
			from = fmt.Sprintf("%d%%", *value.FromPercent)
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%d%%\t%s\n", value.Queue, from, value.ToPercent, value.Destination.ClusterName)
		if err := writer.Flush(); err != nil {
			return err
		}
		if value.Next != "" {
			_, err := fmt.Fprintf(c.Output, "\nNEXT\n\n%s\n", value.Next)
			return err
		}
		if value.Note != "" {
			_, err := fmt.Fprintf(c.Output, "\n%s\n", value.Note)
			return err
		}
		return nil
	case QueueStatus:
		if err := c.printQueueStatuses([]QueueStatus{value}); err != nil {
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
		_, err := fmt.Fprint(c.Output, "No queue migrations configured.\n\nTo configure one, run:\n\n  cluster-migrator queue configure <queue> --destination-cluster <cluster>\n")
		return err
	}

	writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "QUEUE\tROUTING\tDESTINATION")
	for _, status := range statuses {
		_, _ = fmt.Fprintf(writer, "%s\t%d%%\t%s\n", status.Queue, status.RoutingPercent, status.Destination.ClusterName)
	}
	return writer.Flush()
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

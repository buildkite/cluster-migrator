package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		commandHeading := "QUEUE " + value.Command
		if value.DryRun {
			commandHeading += " (DRY RUN)"
		}
		sectionHeading := "RESULT"
		if value.DryRun {
			sectionHeading = "PROPOSED CHANGE"
		}
		if _, err := fmt.Fprintf(c.Output, "%s\nQueue: %s\nDestination: %s\n\n%s\n\n", commandHeading, value.Queue, value.Destination.ClusterName, sectionHeading); err != nil {
			return err
		}
		if value.Command == "CONFIGURE" {
			result := "Migration created with 0% routing."
			if value.DryRun {
				result = "A migration would be created with 0% routing."
			}
			if _, err := fmt.Fprintln(c.Output, result); err != nil {
				return err
			}
		} else if value.FromPercent != nil {
			verb := "changed"
			if value.DryRun {
				verb = "would change"
			}
			if *value.FromPercent == value.ToPercent {
				verb = "remains"
				if value.DryRun {
					verb = "would remain"
				}
				if _, err := fmt.Fprintf(c.Output, "Routing %s at %d%%.\n", verb, value.ToPercent); err != nil {
					return err
				}
			} else if _, err := fmt.Fprintf(c.Output, "Routing %s from %d%% to %d%%.\n", verb, *value.FromPercent, value.ToPercent); err != nil {
				return err
			}
		}
		if value.Next != "" {
			_, err := fmt.Fprintf(c.Output, "\nNEXT STEPS\n\n%s\n", value.Next)
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
		_, err := fmt.Fprint(c.Output, "QUEUE STATUS\nMigrations: 0\n\nSTATUS\n\nNo queue migrations are configured.\n")
		return err
	}
	if _, err := fmt.Fprintf(c.Output, "QUEUE STATUS\nMigrations: %d\n\nSTATUS\n\n", len(statuses)); err != nil {
		return err
	}
	writer := tabwriter.NewWriter(c.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "QUEUE\tROUTING\tDESTINATION")
	for _, status := range statuses {
		_, _ = fmt.Fprintf(writer, "%s\t%d%%\t%s\n", status.Queue, status.RoutingPercent, status.Destination.ClusterName)
	}
	return writer.Flush()
}

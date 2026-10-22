package cli

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func printMigrationAudit(app *Context, migration *buildkite.OrganizationMigration) error {
	if _, err := fmt.Fprintf(app.Output, "\nAUDIT\n\nConfiguration creator: %s\nConfigured at: %s\nPhase version: %s\n", displayValue(migration.CreatedByID), displayValue(migration.CreatedAt), optionalString(migration.PhaseVersion)); err != nil {
		return err
	}
	event := migration.LatestTransition
	if event == nil {
		_, err := fmt.Fprint(app.Output, "Latest phase transition: not provided\n")
		return err
	}
	actor := "system"
	if event.ActorID != nil {
		actor = *event.ActorID
	}
	_, err := fmt.Fprintf(app.Output, "Latest phase transition: %s\nTransition: %s → %s\nSource: %s\nActor: %s\nOccurred at: %s\n", event.EventID, event.From, event.To, event.Source, actor, event.OccurredAt)
	return err
}

func printMigrationProgress(app *Context, progress *buildkite.MigrationProgress) error {
	if progress == nil {
		_, err := fmt.Fprint(app.Output, "\nSTATUS\n\nBackground progress and blockers are not provided by this server.\n")
		return err
	}
	if groups := progress.ConcurrencyGroups; groups != nil {
		if err := printMigrationProgressSummary(app, "CONCURRENCY-GROUP PROGRESS", groups.MigrationProgressSummary); err != nil {
			return err
		}
		if groups.Sample != nil {
			if _, err := fmt.Fprint(app.Output, "Held overlaps draining and reconciling; do not add these counts.\n"); err != nil {
				return err
			}
		}
		if len(groups.Items) > 0 {
			writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "\nGROUP\tSTATE\tHELD\tBLOCKERS")
			for _, item := range groups.Items {
				_, _ = fmt.Fprintf(writer, "%s/%s\t%s\t%t\t%s\n", item.Scope, item.Key, item.State, item.Held, migrationBlockers(item.Blockers))
			}
			if err := writer.Flush(); err != nil {
				return err
			}
		}
	}
	if pipelines := progress.Pipelines; pipelines != nil {
		if err := printMigrationProgressSummary(app, "PIPELINE PROGRESS", pipelines.MigrationProgressSummary); err != nil {
			return err
		}
		if _, err := fmt.Fprint(app.Output, "Completed means currently in the fixed destination, not necessarily moved by this operation.\n"); err != nil {
			return err
		}
		if len(pipelines.Items) > 0 {
			writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "\nPIPELINE\tID\tARCHIVED\tSTATE\tBLOCKERS")
			for _, item := range pipelines.Items {
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%t\t%s\t%s\n", item.Slug, item.PipelineID, item.Archived, item.State, migrationBlockers(item.Blockers))
			}
			if err := writer.Flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

func printMigrationProgressSummary(app *Context, heading string, summary buildkite.MigrationProgressSummary) error {
	if _, err := fmt.Fprintf(app.Output, "\n%s\n\n", heading); err != nil {
		return err
	}
	observation := summary.Observation
	if authorization := summary.Authorization; authorization != nil {
		actor := "system"
		if authorization.ActorID != nil {
			actor = *authorization.ActorID
		}
		if _, err := fmt.Fprintf(app.Output, "Authorization event: %s\nAuthorized at: %s\nActor: %s\n", authorization.EventID, authorization.AuthorizedAt, actor); err != nil {
			return err
		}
	} else if observation == nil || observation.Status == "unavailable" {
		if _, err := fmt.Fprint(app.Output, "Authorization: unavailable\n"); err != nil {
			return err
		}
	} else if _, err := fmt.Fprint(app.Output, "Authorization: not recorded\n"); err != nil {
		return err
	}
	if observation != nil {
		if _, err := fmt.Fprintf(app.Output, "Observation: %s\nObserved at: %s\nExpires at: %s\nCached: %t\n", observation.Status, optionalString(observation.ObservedAt), optionalString(observation.ExpiresAt), observation.Cached); err != nil {
			return err
		}
	}
	if summary.Sample == nil || observation == nil || observation.Status == "unavailable" {
		_, err := fmt.Fprint(app.Output, "Sample unavailable; no counts can be inferred.\n")
		return err
	}
	sample := summary.Sample
	if _, err := fmt.Fprintf(app.Output, "\nSAMPLE (NOT ORGANIZATION TOTALS)\n\nScanned: %d (limit %d)\nMore inventory: %t\nObservation freshness does not establish migration readiness or authorization.\n\n", sample.Scanned, sample.Limit, sample.HasMore); err != nil {
		return err
	}
	keys := make([]string, 0, len(sample.Counts))
	for key := range sample.Counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writer := tabwriter.NewWriter(app.Output, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "STATE\tSAMPLE COUNT")
	for _, key := range keys {
		_, _ = fmt.Fprintf(writer, "%s\t%d\n", key, sample.Counts[key])
	}
	return writer.Flush()
}

func migrationBlockers(blockers []buildkite.MigrationBlocker) string {
	if len(blockers) == 0 {
		return "—"
	}
	messages := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		messages = append(messages, blocker.Code+": "+blocker.Message)
	}
	return strings.Join(messages, "; ")
}

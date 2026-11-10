package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

func (app *Context) timeoutError(parsed *kong.Context, err error) error {
	var timeout *buildkite.RequestTimeout
	if !errors.As(err, &timeout) {
		return err
	}
	command, recovery := timeoutCommands(parsed)
	result := "Read failed; current information is unavailable."
	next := "Retry the read-only inspection when the service recovers:"
	if timeout.Method != http.MethodGet && timeout.Method != http.MethodHead {
		result = "Mutation outcome is unknown; the request may have been accepted. A timeout does not cancel server-side work."
		next = "Do not retry the mutation until you inspect its state:"
	} else if !timeout.MutationAttempted {
		result += " No mutation was attempted."
	}
	information := "Unavailable; no successful state observations were retained."
	if len(app.LastKnown) > 0 {
		information = strings.Join(app.LastKnown, "\n") + "\nThese values were read before the failed request; they are not confirmed current state."
	}
	return fmt.Errorf("%w\n\nCOMMAND\n\n%s\n\nRESULT\n\nRequest: %s %s\n%s\n\nLAST KNOWN INFORMATION\n\n%s\n\nNEXT STEPS\n\n1. %s\n\n   %s\n\n2. If inspection is unavailable or inconclusive, contact Buildkite before another mutation", err, command, timeout.Method, timeout.Path, result, information, next, recovery)
}

func timeoutCommands(parsed *kong.Context) (string, string) {
	command := []string{"cluster-migrator"}
	var arguments []string
	for _, part := range parsed.Path {
		switch {
		case part.Command != nil:
			command = append(command, part.Command.Name)
		case part.Positional != nil:
			value := quoteCommandArgument(part.Positional.Target.String())
			arguments = append(arguments, value)
			command = append(command, value)
		case part.Flag != nil:
			switch part.Flag.Name {
			case "to", "destination-cluster", "acknowledge-irreversible", "dry-run", "json", "timeout":
				command = append(command, "--"+part.Flag.Name+"="+quoteCommandArgument(fmt.Sprint(part.Flag.Target.Interface())))
			}
		}
	}
	recovery := "cluster-migrator migration status"
	switch path := parsed.Command(); {
	case strings.HasPrefix(path, "queue metrics"):
		recovery = "cluster-migrator queue metrics"
	case strings.HasPrefix(path, "queue"):
		recovery = "cluster-migrator queue status"
	case strings.HasPrefix(path, "pipeline"):
		recovery = "cluster-migrator pipeline readiness"
	case strings.HasPrefix(path, "concurrency-group"):
		recovery = "cluster-migrator concurrency-group status"
	}
	if len(arguments) > 0 {
		recovery += " " + strings.Join(arguments, " ")
	}
	return strings.Join(command, " "), recovery
}

func quoteCommandArgument(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\r\n'\"\\$`;&|<>()*?[]{}!#~") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (app *Context) rememberMigration(migration *buildkite.Migration) {
	app.LastKnown = []string{"Migration phase: " + migration.Phase, "Destination: " + migration.DestinationClusterID}
}

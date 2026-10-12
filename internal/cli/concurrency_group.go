package cli

import (
	"fmt"
	"strings"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type ConcurrencyGroupCmd struct {
	Status ConcurrencyGroupStatusCmd `cmd:"" help:"Show one or all unclustered concurrency groups."`
}

// parseConcurrencyGroup splits a <scope>/<key> identifier. Keys may contain slashes; scopes cannot.
func parseConcurrencyGroup(group string) (scope, key string, err error) {
	scope, key, _ = strings.Cut(group, "/")
	if (scope != "custom" && scope != "step") || key == "" {
		return "", "", fmt.Errorf("concurrency group %q must be <scope>/<key>, where scope is custom or step", group)
	}
	return scope, key, nil
}

func concurrencyGroupName(group *buildkite.ConcurrencyGroup) string {
	if group.Scope == "" {
		return displayValue(group.Key)
	}
	return group.Scope + "/" + group.Key
}

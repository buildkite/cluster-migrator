package cli

import (
	"fmt"
	"strings"

	"github.com/buildkite/cluster-migrator/internal/buildkite"
)

type ConcurrencyGroupCmd struct {
	Cutover ConcurrencyGroupCutoverCmd `cmd:"" help:"Atomically hold, drain, and move a concurrency group."`
	Status  ConcurrencyGroupStatusCmd  `cmd:"" help:"Show one or all concurrency groups."`
}

// parseConcurrencyGroup splits a <scope>/<key> identifier. Keys may contain slashes; scopes cannot.
func parseConcurrencyGroup(group string) (scope, key string, err error) {
	scope, key, _ = strings.Cut(group, "/")
	if (scope != "custom" && scope != "step") || key == "" {
		return "", "", fmt.Errorf("concurrency group %q must be <scope>/<key>, where scope is custom or step", group)
	}
	return scope, key, nil
}

func concurrencyGroupName(group buildkite.ConcurrencyGroup) string {
	if group.Scope == "" {
		return group.Key
	}
	return group.Scope + "/" + group.Key
}

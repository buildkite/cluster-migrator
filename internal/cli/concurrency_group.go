package cli

type ConcurrencyGroupCmd struct {
	Cutover ConcurrencyGroupCutoverCmd `cmd:"" help:"Atomically hold, drain, and move a concurrency group."`
	Status  ConcurrencyGroupStatusCmd  `cmd:"" help:"Show one or all concurrency groups."`
}

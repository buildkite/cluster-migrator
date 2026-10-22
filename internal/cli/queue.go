package cli

type QueueCmd struct {
	Configure  QueueConfigureCmd  `cmd:"" help:"Map a source queue to an existing queue in the fixed destination at 0%, during preparation."`
	SetPercent QueueSetPercentCmd `cmd:"" name:"set-percent" help:"Set absolute routing for new jobs during queue preparation."`
	Status     QueueStatusCmd     `cmd:"" help:"Show one or all queue migrations."`
	Metrics    QueueMetricsCmd    `cmd:"" help:"Show recent destination queue activity."`
}

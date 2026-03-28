module agentmail/services/scheduler

go 1.23

require (
	agentmail/pkg v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
	github.com/robfig/cron/v3 v3.0.1
)

replace agentmail/pkg => ../../pkg

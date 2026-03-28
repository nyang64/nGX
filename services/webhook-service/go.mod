module agentmail/services/webhook-service

go 1.23

require (
	agentmail/pkg v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
	github.com/segmentio/kafka-go v0.4.47
)

replace agentmail/pkg => ../../pkg

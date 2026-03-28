module agentmail/services/email-pipeline

go 1.23

require (
	agentmail/pkg v0.0.0
	github.com/emersion/go-smtp v0.21.3
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
	github.com/segmentio/kafka-go v0.4.47
)

require (
	blitiri.com.ar/go/spf v1.4.0 // indirect
	github.com/emersion/go-msgauth v0.6.8 // indirect
)

replace agentmail/pkg => ../../pkg

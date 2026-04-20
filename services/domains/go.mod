module agentmail/services/domains

go 1.23

require (
	agentmail/pkg v0.0.0
	github.com/aws/aws-sdk-go-v2 v1.30.0
	github.com/aws/aws-sdk-go-v2/config v1.27.0
	github.com/aws/aws-sdk-go-v2/service/ses v1.22.0
	github.com/go-chi/chi/v5 v5.1.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
)

replace agentmail/pkg => ../../pkg

module agentmail/services/search

go 1.23

require (
	agentmail/pkg v0.0.0
	github.com/go-chi/chi/v5 v5.1.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
)

replace agentmail/pkg => ../../pkg

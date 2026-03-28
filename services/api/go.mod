module agentmail/services/api

go 1.23

require (
	agentmail/pkg v0.0.0
	github.com/go-chi/chi/v5 v5.1.0
	github.com/go-chi/cors v1.2.1
	github.com/gorilla/websocket v1.5.3
	github.com/jackc/pgx/v5 v5.6.0
	github.com/google/uuid v1.6.0
	github.com/redis/go-redis/v9 v9.5.4
)

replace agentmail/pkg => ../../pkg

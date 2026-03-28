module agentmail/pkg

go 1.23

require (
	github.com/aws/aws-sdk-go-v2 v1.30.0
	github.com/aws/aws-sdk-go-v2/config v1.27.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.57.0
	github.com/emersion/go-msgauth v0.6.4
	github.com/go-playground/validator/v10 v10.22.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.6.0
	github.com/oklog/ulid/v2 v2.1.0
	github.com/redis/go-redis/v9 v9.5.4
	github.com/segmentio/kafka-go v0.4.47
	go.opentelemetry.io/otel v1.28.0
	go.opentelemetry.io/otel/sdk v1.28.0
	go.opentelemetry.io/otel/trace v1.28.0
	golang.org/x/crypto v0.25.0
)

require (
	github.com/aws/aws-sdk-go-v2/credentials v1.17.0 // indirect
	github.com/aws/smithy-go v1.20.2 // indirect
)

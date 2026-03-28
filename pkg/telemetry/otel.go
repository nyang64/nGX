package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
)

// Setup initialises OpenTelemetry tracing and returns a shutdown function.
// If endpoint is empty a no-op tracer is installed and the shutdown function
// is a no-op — this allows callers to always call shutdown without nil-checks.
func Setup(ctx context.Context, serviceName, endpoint string) (func(context.Context), error) {
	if endpoint == "" {
		return func(context.Context) {}, nil
	}

	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	return func(ctx context.Context) {
		_ = tp.Shutdown(ctx)
	}, nil
}

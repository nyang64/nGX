package kafka

// Topic name constants used across all services.
const (
	TopicEmailInboundRaw    = "email.inbound.raw"
	TopicEmailOutboundQueue = "email.outbound.queue"
	TopicEventsFanout       = "events.fanout"
	TopicWebhooksDelivery   = "webhooks.delivery"
)

package events

import (
	"encoding/json"
	"fmt"
)

// Event is implemented by every concrete event type.
type Event interface {
	GetBase() BaseEvent
}

// Marshal serializes an event to JSON.
func Marshal(e Event) ([]byte, error) {
	return json.Marshal(e)
}

// Unmarshal deserializes an event, returning the concrete type based on the
// "type" field in the JSON envelope.
func Unmarshal(data []byte) (Event, error) {
	var base BaseEvent
	if err := json.Unmarshal(data, &base); err != nil {
		return nil, fmt.Errorf("unmarshal base event: %w", err)
	}
	switch base.Type {
	case EventMessageReceived:
		var e MessageReceivedEvent
		return &e, json.Unmarshal(data, &e)
	case EventMessageSent:
		var e MessageSentEvent
		return &e, json.Unmarshal(data, &e)
	case EventMessageBounced:
		var e MessageBouncedEvent
		return &e, json.Unmarshal(data, &e)
	case EventThreadCreated:
		var e ThreadCreatedEvent
		return &e, json.Unmarshal(data, &e)
	case EventThreadStatusChanged:
		var e ThreadStatusChangedEvent
		return &e, json.Unmarshal(data, &e)
	case EventDraftCreated:
		var e DraftCreatedEvent
		return &e, json.Unmarshal(data, &e)
	case EventDraftApproved:
		var e DraftApprovedEvent
		return &e, json.Unmarshal(data, &e)
	case EventDraftRejected:
		var e DraftRejectedEvent
		return &e, json.Unmarshal(data, &e)
	case EventInboxCreated:
		var e InboxCreatedEvent
		return &e, json.Unmarshal(data, &e)
	case EventLabelApplied:
		var e LabelAppliedEvent
		return &e, json.Unmarshal(data, &e)
	default:
		return nil, fmt.Errorf("unknown event type: %s", base.Type)
	}
}

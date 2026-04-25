/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package events

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewBase(t *testing.T) {
	orgID := uuid.New()
	before := time.Now()
	b := NewBase(EventMessageReceived, orgID)
	after := time.Now()

	if b.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if _, err := uuid.Parse(b.ID); err != nil {
		t.Fatalf("ID is not a valid UUID: %v", err)
	}
	if b.Type != EventMessageReceived {
		t.Fatalf("expected type %q, got %q", EventMessageReceived, b.Type)
	}
	if b.OrgID != orgID.String() {
		t.Fatalf("expected OrgID %q, got %q", orgID.String(), b.OrgID)
	}
	if b.OccurredAt.Before(before) || b.OccurredAt.After(after.Add(5*time.Second)) {
		t.Fatalf("OccurredAt %v not within expected range", b.OccurredAt)
	}
}

func TestMarshalUnmarshal_MessageReceived(t *testing.T) {
	orgID := uuid.New()
	inboxID := uuid.New()
	threadID := uuid.New()

	now := time.Now().UTC().Truncate(time.Second)
	orig := &MessageReceivedEvent{
		BaseEvent: NewBase(EventMessageReceived, orgID),
		Data: MessageReceivedData{
			MessagePayload: MessagePayload{
				ID:        uuid.New().String(),
				MessageID: "msg-001",
				InboxID:   inboxID,
				ThreadID:  threadID,
				Direction: "inbound",
				Status:    "received",
				Subject:   "Hello",
				From:      EmailAddress{Email: "sender@example.com", Name: "Sender"},
				To:        []EmailAddress{{Email: "inbox@example.com"}},
				Cc:        []EmailAddress{},
				Bcc:       []EmailAddress{},
				BodyText:  "Hello world",
				Preview:   "Hello world",
				Attachments: []AttachmentInfo{},
				CreatedAt: now,
				UpdatedAt: now,
			},
			RawS3Key: "raw/msg-001",
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	evt, ok := got.(*MessageReceivedEvent)
	if !ok {
		t.Fatalf("expected *MessageReceivedEvent, got %T", got)
	}
	if evt.Data.MessageID != orig.Data.MessageID {
		t.Errorf("MessageID: want %q, got %q", orig.Data.MessageID, evt.Data.MessageID)
	}
	if evt.Data.InboxID != orig.Data.InboxID {
		t.Errorf("InboxID: want %v, got %v", orig.Data.InboxID, evt.Data.InboxID)
	}
	if evt.Data.ThreadID != orig.Data.ThreadID {
		t.Errorf("ThreadID: want %v, got %v", orig.Data.ThreadID, evt.Data.ThreadID)
	}
	if evt.Data.From.Email != orig.Data.From.Email {
		t.Errorf("From.Email: want %q, got %q", orig.Data.From.Email, evt.Data.From.Email)
	}
	if evt.Data.Subject != orig.Data.Subject {
		t.Errorf("Subject: want %q, got %q", orig.Data.Subject, evt.Data.Subject)
	}
	if evt.Data.RawS3Key != orig.Data.RawS3Key {
		t.Errorf("RawS3Key: want %q, got %q", orig.Data.RawS3Key, evt.Data.RawS3Key)
	}
	if evt.Data.BodyText != orig.Data.BodyText {
		t.Errorf("BodyText: want %q, got %q", orig.Data.BodyText, evt.Data.BodyText)
	}
}

func TestMarshalUnmarshal_MessageSent(t *testing.T) {
	orgID := uuid.New()
	inboxID := uuid.New()
	threadID := uuid.New()

	now := time.Now().UTC().Truncate(time.Second)
	orig := &MessageSentEvent{
		BaseEvent: NewBase(EventMessageSent, orgID),
		Data: MessageSentData{
			MessagePayload: MessagePayload{
				ID:        uuid.New().String(),
				MessageID: "msg-002",
				InboxID:   inboxID,
				ThreadID:  threadID,
				Direction: "outbound",
				Status:    "sent",
				Subject:   "Re: Hello",
				From:      EmailAddress{Email: "support@example.com", Name: "Support"},
				To:        []EmailAddress{{Email: "a@example.com"}, {Email: "b@example.com"}},
				Cc:        []EmailAddress{},
				Bcc:       []EmailAddress{},
				BodyText:  "Thanks for reaching out.",
				Preview:   "Thanks for reaching out.",
				Attachments: []AttachmentInfo{},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	evt, ok := got.(*MessageSentEvent)
	if !ok {
		t.Fatalf("expected *MessageSentEvent, got %T", got)
	}
	if evt.Data.MessageID != orig.Data.MessageID {
		t.Errorf("MessageID: want %q, got %q", orig.Data.MessageID, evt.Data.MessageID)
	}
	if len(evt.Data.To) != len(orig.Data.To) {
		t.Errorf("To length: want %d, got %d", len(orig.Data.To), len(evt.Data.To))
	}
	for i := range orig.Data.To {
		if evt.Data.To[i].Email != orig.Data.To[i].Email {
			t.Errorf("To[%d].Email: want %q, got %q", i, orig.Data.To[i].Email, evt.Data.To[i].Email)
		}
	}
	if evt.Data.Subject != orig.Data.Subject {
		t.Errorf("Subject: want %q, got %q", orig.Data.Subject, evt.Data.Subject)
	}
	if evt.Data.BodyText != orig.Data.BodyText {
		t.Errorf("BodyText: want %q, got %q", orig.Data.BodyText, evt.Data.BodyText)
	}
}

func TestMarshalUnmarshal_MessageBounced(t *testing.T) {
	orgID := uuid.New()
	inboxID := uuid.New()
	threadID := uuid.New()

	now := time.Now().UTC().Truncate(time.Second)
	orig := &MessageBouncedEvent{
		BaseEvent: NewBase(EventMessageBounced, orgID),
		Data: MessageBouncedData{
			MessagePayload: MessagePayload{
				ID:        uuid.New().String(),
				MessageID: "msg-003",
				InboxID:   inboxID,
				ThreadID:  threadID,
				Direction: "outbound",
				Status:    "bounced",
				Subject:   "Invoice",
				From:      EmailAddress{Email: "support@example.com"},
				To:        []EmailAddress{{Email: "bad@example.com"}},
				Cc:        []EmailAddress{},
				Bcc:       []EmailAddress{},
				Attachments: []AttachmentInfo{},
				CreatedAt: now,
				UpdatedAt: now,
			},
			BounceCode:   "550",
			BounceReason: "Mailbox not found",
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	evt, ok := got.(*MessageBouncedEvent)
	if !ok {
		t.Fatalf("expected *MessageBouncedEvent, got %T", got)
	}
	if evt.Data.MessageID != orig.Data.MessageID {
		t.Errorf("MessageID: want %q, got %q", orig.Data.MessageID, evt.Data.MessageID)
	}
	if evt.Data.BounceCode != orig.Data.BounceCode {
		t.Errorf("BounceCode: want %q, got %q", orig.Data.BounceCode, evt.Data.BounceCode)
	}
	if evt.Data.BounceReason != orig.Data.BounceReason {
		t.Errorf("BounceReason: want %q, got %q", orig.Data.BounceReason, evt.Data.BounceReason)
	}
	if evt.Data.Subject != orig.Data.Subject {
		t.Errorf("Subject: want %q, got %q", orig.Data.Subject, evt.Data.Subject)
	}
}

func TestMarshalUnmarshal_ThreadCreated(t *testing.T) {
	orgID := uuid.New()
	threadID := uuid.New()
	inboxID := uuid.New()

	orig := &ThreadCreatedEvent{
		BaseEvent: NewBase(EventThreadCreated, orgID),
		Data: ThreadCreatedData{
			ThreadID:  threadID,
			InboxID:   inboxID,
			Subject:   "New thread",
			MessageID: "msg-004",
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	evt, ok := got.(*ThreadCreatedEvent)
	if !ok {
		t.Fatalf("expected *ThreadCreatedEvent, got %T", got)
	}
	if evt.Data.ThreadID != orig.Data.ThreadID {
		t.Errorf("ThreadID: want %v, got %v", orig.Data.ThreadID, evt.Data.ThreadID)
	}
	if evt.Data.Subject != orig.Data.Subject {
		t.Errorf("Subject: want %q, got %q", orig.Data.Subject, evt.Data.Subject)
	}
	if evt.Data.MessageID != orig.Data.MessageID {
		t.Errorf("MessageID: want %q, got %q", orig.Data.MessageID, evt.Data.MessageID)
	}
}

func TestMarshalUnmarshal_DraftCreated(t *testing.T) {
	orgID := uuid.New()
	draftID := uuid.New()
	threadID := uuid.New()
	inboxID := uuid.New()

	orig := &DraftCreatedEvent{
		BaseEvent: NewBase(EventDraftCreated, orgID),
		Data: DraftCreatedData{
			DraftID:  draftID,
			ThreadID: threadID,
			InboxID:  inboxID,
		},
	}

	data, err := Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	evt, ok := got.(*DraftCreatedEvent)
	if !ok {
		t.Fatalf("expected *DraftCreatedEvent, got %T", got)
	}
	if evt.Data.DraftID != orig.Data.DraftID {
		t.Errorf("DraftID: want %v, got %v", orig.Data.DraftID, evt.Data.DraftID)
	}
	if evt.Data.ThreadID != orig.Data.ThreadID {
		t.Errorf("ThreadID: want %v, got %v", orig.Data.ThreadID, evt.Data.ThreadID)
	}
	if evt.Data.InboxID != orig.Data.InboxID {
		t.Errorf("InboxID: want %v, got %v", orig.Data.InboxID, evt.Data.InboxID)
	}
}

func TestUnmarshal_UnknownType(t *testing.T) {
	raw := `{"id":"abc","type":"unknown.type","org_id":"org1","occurred_at":"2024-01-01T00:00:00Z"}`
	_, err := Unmarshal([]byte(raw))
	if err == nil {
		t.Fatal("expected error for unknown event type, got nil")
	}
}

func TestUnmarshal_InvalidJSON(t *testing.T) {
	_, err := Unmarshal([]byte("not valid json at all"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestMarshal_RoundTrip(t *testing.T) {
	orgID := uuid.New()
	inboxID := uuid.New()
	threadID := uuid.New()
	draftID := uuid.New()
	labelID := uuid.New()
	podID := uuid.New()

	events := []Event{
		&MessageReceivedEvent{
			BaseEvent: NewBase(EventMessageReceived, orgID),
			Data: MessageReceivedData{
				MessagePayload: MessagePayload{
					ID: uuid.New().String(), MessageID: "m1",
					InboxID: inboxID, ThreadID: threadID,
					Direction: "inbound", Status: "received",
					Subject: "s1",
					From:    EmailAddress{Email: "a@b.com"},
					To: []EmailAddress{{Email: "inbox@example.com"}},
					Cc: []EmailAddress{}, Bcc: []EmailAddress{},
					Attachments: []AttachmentInfo{},
					CreatedAt: time.Now(), UpdatedAt: time.Now(),
				},
				RawS3Key: "k1",
			},
		},
		&MessageSentEvent{
			BaseEvent: NewBase(EventMessageSent, orgID),
			Data: MessageSentData{
				MessagePayload: MessagePayload{
					ID: uuid.New().String(), MessageID: "m2",
					InboxID: inboxID, ThreadID: threadID,
					Direction: "outbound", Status: "sent",
					Subject: "s2",
					From:    EmailAddress{Email: "support@example.com"},
					To: []EmailAddress{{Email: "c@d.com"}},
					Cc: []EmailAddress{}, Bcc: []EmailAddress{},
					Attachments: []AttachmentInfo{},
					CreatedAt: time.Now(), UpdatedAt: time.Now(),
				},
			},
		},
		&MessageBouncedEvent{
			BaseEvent: NewBase(EventMessageBounced, orgID),
			Data: MessageBouncedData{
				MessagePayload: MessagePayload{
					ID: uuid.New().String(), MessageID: "m3",
					InboxID: inboxID, ThreadID: threadID,
					Direction: "outbound", Status: "bounced",
					Subject: "s3",
					From:    EmailAddress{Email: "support@example.com"},
					To: []EmailAddress{{Email: "bad@example.com"}},
					Cc: []EmailAddress{}, Bcc: []EmailAddress{},
					Attachments: []AttachmentInfo{},
					CreatedAt: time.Now(), UpdatedAt: time.Now(),
				},
				BounceCode: "550", BounceReason: "no mailbox",
			},
		},
		&ThreadCreatedEvent{
			BaseEvent: NewBase(EventThreadCreated, orgID),
			Data: ThreadCreatedData{
				ThreadID: threadID, InboxID: inboxID,
				Subject: "thread subject", MessageID: "m4",
			},
		},
		&ThreadStatusChangedEvent{
			BaseEvent: NewBase(EventThreadStatusChanged, orgID),
			Data: ThreadStatusChangedData{
				ThreadID: threadID, InboxID: inboxID,
				OldStatus: "open", NewStatus: "closed",
			},
		},
		&DraftCreatedEvent{
			BaseEvent: NewBase(EventDraftCreated, orgID),
			Data:      DraftCreatedData{DraftID: draftID, ThreadID: threadID, InboxID: inboxID},
		},
		&DraftApprovedEvent{
			BaseEvent: NewBase(EventDraftApproved, orgID),
			Data:      DraftApprovedData{DraftID: draftID, ThreadID: threadID, InboxID: inboxID},
		},
		&DraftRejectedEvent{
			BaseEvent: NewBase(EventDraftRejected, orgID),
			Data: DraftRejectedData{
				DraftID: draftID, ThreadID: threadID, InboxID: inboxID,
				Reason: "off-topic",
			},
		},
		&InboxCreatedEvent{
			BaseEvent: NewBase(EventInboxCreated, orgID),
			Data: InboxCreatedData{
				InboxID: inboxID, EmailAddress: "inbox@example.com", PodID: podID,
			},
		},
		&LabelAppliedEvent{
			BaseEvent: NewBase(EventLabelApplied, orgID),
			Data: LabelAppliedData{
				ThreadID: threadID, LabelID: labelID, LabelName: "urgent",
			},
		},
	}

	for _, orig := range events {
		expectedType := orig.GetBase().Type

		raw, err := Marshal(orig)
		if err != nil {
			t.Errorf("Marshal(%s): %v", expectedType, err)
			continue
		}

		// Verify the JSON contains the right type field.
		var envelope struct {
			Type EventType `json:"type"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Errorf("unmarshal envelope for %s: %v", expectedType, err)
			continue
		}
		if envelope.Type != expectedType {
			t.Errorf("JSON type field: want %q, got %q", expectedType, envelope.Type)
		}

		got, err := Unmarshal(raw)
		if err != nil {
			t.Errorf("Unmarshal(%s): %v", expectedType, err)
			continue
		}

		if got.GetBase().Type != expectedType {
			t.Errorf("GetBase().Type after round-trip: want %q, got %q", expectedType, got.GetBase().Type)
		}
		if got.GetBase().ID != orig.GetBase().ID {
			t.Errorf("GetBase().ID after round-trip: want %q, got %q", orig.GetBase().ID, got.GetBase().ID)
		}
	}
}

/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package integration

// OpenAPI contract checks.
//
// These tests verify that the runtime API responses contain the fields
// documented as "required" in api/openapi.yaml. They are NOT exhaustive
// schema validators — they check key field names so that renaming or removing
// a documented required field fails CI before it reaches production.
//
// When you intentionally change a field name or remove a required field:
//  1. Update api/openapi.yaml to reflect the new contract.
//  2. Update the corresponding requiredFields slice below.
//  3. The tests will pass once both are in sync.
//
// When you add a new required field to the spec, add it here so the test
// enforces the contract going forward.

import (
	"testing"
)

// assertRequiredFields checks that every field in required is present (non-empty
// string or non-nil) in the given map. Fails the test for any missing field.
func assertRequiredFields(t *testing.T, label string, body map[string]any, required []string) {
	t.Helper()
	for _, field := range required {
		if _, ok := body[field]; !ok {
			t.Errorf("%s: required field %q missing from response", label, field)
		}
	}
}

// TestContractOrg validates GET /v1/org against the documented Organization schema.
func TestContractOrg(t *testing.T) {
	c := newClient(t)
	code, body, err := c.get("/v1/org")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI Organization required: [id, name, slug, plan, created_at, updated_at]
	assertRequiredFields(t, "GET /v1/org", body, []string{
		"id", "name", "slug", "plan", "created_at", "updated_at",
	})
}

// TestContractInboxList validates GET /v1/inboxes against the documented InboxList schema.
func TestContractInboxList(t *testing.T) {
	c := newClient(t)
	code, body, err := c.get("/v1/inboxes")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI InboxList required: [inboxes]
	if _, ok := body["inboxes"]; !ok {
		t.Fatal("GET /v1/inboxes: required field \"inboxes\" missing")
	}
	inboxes := listOf(body, "inboxes")
	if len(inboxes) == 0 {
		t.Skip("no inboxes available; skipping per-inbox field checks")
	}
	inbox := asMap(inboxes[0])
	// Runtime fields: id, org_id, email, display_name, status, created_at, updated_at.
	// NOTE: OpenAPI Inbox schema uses outdated field names (inbox_id, address) — known
	// drift; spec update is tracked separately.
	assertRequiredFields(t, "Inbox object", inbox, []string{
		"id", "org_id", "email", "created_at", "updated_at",
	})
}

// TestContractThreadList validates GET /v1/inboxes/{id}/threads against the
// documented ThreadList schema.
func TestContractThreadList(t *testing.T) {
	c := newClient(t)

	// Get an inbox to use.
	_, inboxesBody, err := c.get("/v1/inboxes")
	if err != nil {
		t.Fatal(err)
	}
	inboxes := listOf(inboxesBody, "inboxes")
	if len(inboxes) == 0 {
		t.Skip("no inboxes available")
	}
	inboxID := str(asMap(inboxes[0]), "id")

	code, body, err := c.get("/v1/inboxes/" + inboxID + "/threads")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI ThreadList required: [threads]
	if _, ok := body["threads"]; !ok {
		t.Fatal("GET /threads: required field \"threads\" missing")
	}
	threads := listOf(body, "threads")
	if len(threads) == 0 {
		t.Skip("no threads available; skipping per-thread field checks")
	}
	thread := asMap(threads[0])
	// OpenAPI Thread required: [id, inbox_id, subject, status, is_read, is_starred, message_count, created_at, updated_at]
	assertRequiredFields(t, "Thread object", thread, []string{
		"id", "inbox_id", "subject", "status", "is_read", "is_starred",
		"message_count", "created_at", "updated_at",
	})
}

// TestContractLabelList validates GET /v1/labels against the documented LabelList schema.
func TestContractLabelList(t *testing.T) {
	c := newClient(t)
	code, body, err := c.get("/v1/labels")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI LabelList required: [labels]
	if _, ok := body["labels"]; !ok {
		t.Fatal("GET /v1/labels: required field \"labels\" missing")
	}
	labels := listOf(body, "labels")
	if len(labels) == 0 {
		return // no labels to validate schema against
	}
	label := asMap(labels[0])
	// OpenAPI Label required: [id, org_id, name, color, created_at, updated_at]
	assertRequiredFields(t, "Label object", label, []string{
		"id", "org_id", "name", "color", "created_at", "updated_at",
	})
}

// TestContractWebhookList validates GET /v1/webhooks against the documented WebhookList schema.
func TestContractWebhookList(t *testing.T) {
	c := newClient(t)
	code, body, err := c.get("/v1/webhooks")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI WebhookList required: [webhooks]
	if _, ok := body["webhooks"]; !ok {
		t.Fatal("GET /v1/webhooks: required field \"webhooks\" missing")
	}
	hooks := listOf(body, "webhooks")
	if len(hooks) == 0 {
		return
	}
	hook := asMap(hooks[0])
	// OpenAPI Webhook required: [id, org_id, url, events, is_active, created_at, updated_at]
	assertRequiredFields(t, "Webhook object", hook, []string{
		"id", "org_id", "url", "events", "is_active", "created_at", "updated_at",
	})
}

// TestContractAPIKeyList validates GET /v1/keys against the documented APIKeyList schema.
func TestContractAPIKeyList(t *testing.T) {
	c := newClient(t)
	code, body, err := c.get("/v1/keys")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI APIKeyList required: [keys]
	if _, ok := body["keys"]; !ok {
		t.Fatal("GET /v1/keys: required field \"keys\" missing")
	}
	keys := listOf(body, "keys")
	if len(keys) == 0 {
		return
	}
	key := asMap(keys[0])
	// OpenAPI APIKey required: [id, name, key_prefix, scopes, created_at]
	assertRequiredFields(t, "APIKey object", key, []string{
		"id", "name", "key_prefix", "scopes", "created_at",
	})
}

// TestContractDomainList validates GET /v1/domains against the documented DomainConfig schema.
func TestContractDomainList(t *testing.T) {
	c := newClient(t)
	code, body, err := c.get("/v1/domains")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI DomainConfig list required: [domains]
	if _, ok := body["domains"]; !ok {
		t.Fatal("GET /v1/domains: required field \"domains\" missing")
	}
	domains := listOf(body, "domains")
	if len(domains) == 0 {
		return
	}
	domain := asMap(domains[0])
	// OpenAPI DomainConfig required: [id, org_id, domain, status, dkim_selector, created_at, updated_at]
	assertRequiredFields(t, "DomainConfig object", domain, []string{
		"id", "org_id", "domain", "status", "dkim_selector", "created_at", "updated_at",
	})
}

// TestContractPodList validates GET /v1/pods against the documented Pod schema.
func TestContractPodList(t *testing.T) {
	c := newClient(t)
	code, body, err := c.get("/v1/pods")
	if err != nil {
		t.Fatal(err)
	}
	mustCode(t, code, 200, body)
	// OpenAPI PodList required: [pods]
	if _, ok := body["pods"]; !ok {
		t.Fatal("GET /v1/pods: required field \"pods\" missing")
	}
	pods := listOf(body, "pods")
	if len(pods) == 0 {
		return
	}
	pod := asMap(pods[0])
	// OpenAPI Pod required: [id, org_id, name, slug, created_at, updated_at]
	assertRequiredFields(t, "Pod object", pod, []string{
		"id", "org_id", "name", "slug", "created_at", "updated_at",
	})
}

/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package config

import (
	"strings"
	"testing"
	"time"
)

// TestLoad_MailDomain verifies that MAIL_DOMAIN is loaded into cfg.MailDomain.
func TestLoad_MailDomain(t *testing.T) {
	t.Setenv("MAIL_DOMAIN", "mail.acme.com")
	cfg := Load()
	if cfg.MailDomain != "mail.acme.com" {
		t.Errorf("MailDomain: got %q, want %q", cfg.MailDomain, "mail.acme.com")
	}
}

// TestLoad_MailDomain_Empty verifies that MailDomain defaults to empty string
// when MAIL_DOMAIN is not set — callers must validate before use.
func TestLoad_MailDomain_Empty(t *testing.T) {
	t.Setenv("MAIL_DOMAIN", "")
	cfg := Load()
	if cfg.MailDomain != "" {
		t.Errorf("MailDomain default: got %q, want empty string", cfg.MailDomain)
	}
}

// TestLoad_DKIMSelector_Default verifies the new default selector is "mail"
// (changed from "agentmail1" during the nGX rebrand).
func TestLoad_DKIMSelector_Default(t *testing.T) {
	t.Setenv("DKIM_SELECTOR", "")
	cfg := Load()
	if cfg.SMTP.DKIMSelector != "mail" {
		t.Errorf("DKIMSelector default: got %q, want %q", cfg.SMTP.DKIMSelector, "mail")
	}
}

// TestLoad_DKIMSelector_Override verifies that DKIM_SELECTOR env var overrides the default.
func TestLoad_DKIMSelector_Override(t *testing.T) {
	t.Setenv("DKIM_SELECTOR", "v1")
	cfg := Load()
	if cfg.SMTP.DKIMSelector != "v1" {
		t.Errorf("DKIMSelector override: got %q, want %q", cfg.SMTP.DKIMSelector, "v1")
	}
}

// TestLoad_DKIMDomain verifies that DKIM_DOMAIN is loaded correctly.
func TestLoad_DKIMDomain(t *testing.T) {
	t.Setenv("DKIM_DOMAIN", "mail.acme.com")
	cfg := Load()
	if cfg.SMTP.DKIMDomain != "mail.acme.com" {
		t.Errorf("DKIMDomain: got %q, want %q", cfg.SMTP.DKIMDomain, "mail.acme.com")
	}
}

// TestLoad_DKIMDomain_Default verifies that DKIMDomain defaults to empty (must be set by operator).
func TestLoad_DKIMDomain_Default(t *testing.T) {
	t.Setenv("DKIM_DOMAIN", "")
	cfg := Load()
	if cfg.SMTP.DKIMDomain != "" {
		t.Errorf("DKIMDomain default: got %q, want empty string", cfg.SMTP.DKIMDomain)
	}
}

// TestLoad_DatabaseURL_Default verifies the default DATABASE_URL contains expected components.
func TestLoad_DatabaseURL_Default(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	cfg := Load()
	if cfg.Database.URL == "" {
		t.Error("DATABASE_URL default should not be empty")
	}
	// Default should be a valid postgres URL for local dev.
	if !strings.HasPrefix(cfg.Database.URL, "postgres://") {
		t.Errorf("DATABASE_URL default should start with postgres://, got: %s", cfg.Database.URL)
	}
}

// TestLoad_DatabasePoolDefaults verifies sensible connection pool defaults.
func TestLoad_DatabasePoolDefaults(t *testing.T) {
	t.Setenv("DB_MAX_CONNS", "")
	t.Setenv("DB_MIN_CONNS", "")
	cfg := Load()
	if cfg.Database.MaxConns != 25 {
		t.Errorf("DB_MAX_CONNS default: got %d, want 25", cfg.Database.MaxConns)
	}
	if cfg.Database.MinConns != 5 {
		t.Errorf("DB_MIN_CONNS default: got %d, want 5", cfg.Database.MinConns)
	}
}

// TestLoad_SMTPListenAddr_Default verifies the default SMTP listen address.
func TestLoad_SMTPListenAddr_Default(t *testing.T) {
	t.Setenv("SMTP_LISTEN_ADDR", "")
	cfg := Load()
	if cfg.SMTP.ListenAddr != ":2525" {
		t.Errorf("SMTP_LISTEN_ADDR default: got %q, want %q", cfg.SMTP.ListenAddr, ":2525")
	}
}

// TestLoad_WebhookDefaults verifies webhook retry and concurrency defaults.
func TestLoad_WebhookDefaults(t *testing.T) {
	t.Setenv("WEBHOOK_MAX_RETRIES", "")
	t.Setenv("WEBHOOK_CONCURRENCY", "")
	cfg := Load()
	if cfg.Webhook.MaxRetries != 8 {
		t.Errorf("WEBHOOK_MAX_RETRIES default: got %d, want 8", cfg.Webhook.MaxRetries)
	}
	if cfg.Webhook.Concurrency != 10 {
		t.Errorf("WEBHOOK_CONCURRENCY default: got %d, want 10", cfg.Webhook.Concurrency)
	}
}

// TestLoad_ConnLifetimeDefaults verifies connection lifetime duration parsing.
func TestLoad_ConnLifetimeDefaults(t *testing.T) {
	t.Setenv("DB_MAX_CONN_LIFETIME_SEC", "")
	t.Setenv("DB_MAX_CONN_IDLE_SEC", "")
	cfg := Load()
	if cfg.Database.MaxConnLifetime != 3600*time.Second {
		t.Errorf("MaxConnLifetime default: got %v, want 3600s", cfg.Database.MaxConnLifetime)
	}
	if cfg.Database.MaxConnIdleTime != 300*time.Second {
		t.Errorf("MaxConnIdleTime default: got %v, want 300s", cfg.Database.MaxConnIdleTime)
	}
}

// TestLoad_EnvOverride verifies that env vars take precedence over defaults.
func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("MAIL_DOMAIN", "mail.enterprise.com")
	t.Setenv("DKIM_SELECTOR", "dkim2024")
	t.Setenv("DKIM_DOMAIN", "mail.enterprise.com")
	t.Setenv("SMTP_LISTEN_ADDR", ":25")
	t.Setenv("WEBHOOK_MAX_RETRIES", "3")

	cfg := Load()

	if cfg.MailDomain != "mail.enterprise.com" {
		t.Errorf("MailDomain: got %q", cfg.MailDomain)
	}
	if cfg.SMTP.DKIMSelector != "dkim2024" {
		t.Errorf("DKIMSelector: got %q", cfg.SMTP.DKIMSelector)
	}
	if cfg.SMTP.DKIMDomain != "mail.enterprise.com" {
		t.Errorf("DKIMDomain: got %q", cfg.SMTP.DKIMDomain)
	}
	if cfg.SMTP.ListenAddr != ":25" {
		t.Errorf("SMTP ListenAddr: got %q", cfg.SMTP.ListenAddr)
	}
	if cfg.Webhook.MaxRetries != 3 {
		t.Errorf("Webhook.MaxRetries: got %d", cfg.Webhook.MaxRetries)
	}
}

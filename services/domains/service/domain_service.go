/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

// Package service implements custom-domain management for enterprise orgs.
//
// Flow for adding a domain:
//  1. POST /v1/domains  {"domain": "acme.com"}
//  2. SES: VerifyDomainIdentity  → TXT verification token
//  3. SES: VerifyDomainDkim      → 3 CNAME DKIM tokens
//  4. SES: CreateReceiptRule     → route acme.com inbound mail to shared S3 bucket
//  5. DB:  INSERT domain_configs (status=pending)
//  6. Return DNS records to customer
//
// Customer adds DNS records, then:
//  7. POST /v1/domains/{id}/verify
//  8. SES: GetIdentityVerificationAttributes → check status
//  9. DB:  UPDATE status = active | failed
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentmail/pkg/auth"
	dbpkg "agentmail/pkg/db"
	"agentmail/pkg/models"
	"agentmail/services/domains/store"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrDomainNotFound = errors.New("domain not found")

// DomainService manages custom domain lifecycle: SES identity + receipt rules + DB state.
type DomainService struct {
	pool           *pgxpool.Pool
	store          store.DomainStore
	ses            *ses.Client
	ruleSetName    string // e.g. "ngx-prod-receipt-rules"
	s3Bucket       string // shared emails S3 bucket
	awsRegion      string
}

func New(
	pool *pgxpool.Pool,
	domainStore store.DomainStore,
	sesClient *ses.Client,
	ruleSetName, s3Bucket, awsRegion string,
) *DomainService {
	return &DomainService{
		pool:        pool,
		store:       domainStore,
		ses:         sesClient,
		ruleSetName: ruleSetName,
		s3Bucket:    s3Bucket,
		awsRegion:   awsRegion,
	}
}

// RegisterRequest is the input for POST /v1/domains.
type RegisterRequest struct {
	Domain string
	PodID  *uuid.UUID
}

// RegisterResult is the response for POST /v1/domains.
type RegisterResult struct {
	Domain     *models.DomainConfig
	DNSRecords []models.DNSRecord
}

// Register registers a custom domain for an org:
//   - creates SES domain identity
//   - gets DKIM tokens
//   - creates SES receipt rule (routes to shared S3 inbound pipeline)
//   - saves domain_config record (status=pending)
//   - returns DNS records customer must add
func (s *DomainService) Register(ctx context.Context, claims *auth.Claims, req RegisterRequest) (*RegisterResult, error) {
	domain := strings.ToLower(strings.TrimSpace(req.Domain))
	if domain == "" {
		return nil, fmt.Errorf("domain is required")
	}

	// Step 1: Start SES domain verification → returns TXT token.
	verifyOut, err := s.ses.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{
		Domain: aws.String(domain),
	})
	if err != nil {
		return nil, fmt.Errorf("SES VerifyDomainIdentity: %w", err)
	}
	txtToken := aws.ToString(verifyOut.VerificationToken)

	// Step 2: Enable Easy DKIM → returns 3 CNAME tokens.
	dkimOut, err := s.ses.VerifyDomainDkim(ctx, &ses.VerifyDomainDkimInput{
		Domain: aws.String(domain),
	})
	if err != nil {
		return nil, fmt.Errorf("SES VerifyDomainDkim: %w", err)
	}

	// Step 3: Add SES receipt rule to route inbound email for this domain
	// to the shared S3 bucket (same pipeline as the platform domain).
	ruleName := receiptRuleName(domain)
	_, err = s.ses.CreateReceiptRule(ctx, &ses.CreateReceiptRuleInput{
		RuleSetName: aws.String(s.ruleSetName),
		Rule: &types.ReceiptRule{
			Name:        aws.String(ruleName),
			Enabled:     true,
			TlsPolicy:   types.TlsPolicyRequire,
			ScanEnabled: true,
			Recipients:  []string{domain},
			Actions: []types.ReceiptAction{
				{
					S3Action: &types.S3Action{
						BucketName:       aws.String(s.s3Bucket),
						ObjectKeyPrefix:  aws.String("inbound/raw/"),
					},
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("SES CreateReceiptRule: %w", err)
	}

	// Step 4: Persist to DB.
	now := time.Now().UTC()
	domainCfg := &models.DomainConfig{
		ID:           uuid.New(),
		OrgID:        claims.OrgID,
		PodID:        req.PodID,
		Domain:       domain,
		Status:       "pending",
		DKIMSelector: "ses", // SES manages rotation automatically
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	err = dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx dbpkg.Tx) error {
		return s.store.Create(ctx, tx, domainCfg)
	})
	if err != nil {
		// Best-effort cleanup: remove the SES receipt rule we just created.
		if _, delErr := s.ses.DeleteReceiptRule(ctx, &ses.DeleteReceiptRuleInput{
			RuleSetName: aws.String(s.ruleSetName),
			RuleName:    aws.String(ruleName),
		}); delErr != nil {
			slog.Error("rollback SES receipt rule after DB failure", "rule", ruleName, "error", delErr)
		}
		return nil, fmt.Errorf("save domain config: %w", err)
	}

	// Step 5: Build DNS records for the customer.
	dns := buildDNSRecords(domain, txtToken, dkimOut.DkimTokens, s.awsRegion)

	return &RegisterResult{Domain: domainCfg, DNSRecords: dns}, nil
}

// List returns all domains registered for the org.
func (s *DomainService) List(ctx context.Context, claims *auth.Claims) ([]*models.DomainConfig, error) {
	var domains []*models.DomainConfig
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx dbpkg.Tx) error {
		var err error
		domains, err = s.store.List(ctx, tx, claims.OrgID)
		return err
	})
	return domains, err
}

// Get returns a single domain by ID.
func (s *DomainService) Get(ctx context.Context, claims *auth.Claims, domainID uuid.UUID) (*models.DomainConfig, error) {
	var d *models.DomainConfig
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx dbpkg.Tx) error {
		var err error
		d, err = s.store.GetByID(ctx, tx, claims.OrgID, domainID)
		return err
	})
	return d, err
}

// VerifyResult is the response for POST /v1/domains/{id}/verify.
type VerifyResult struct {
	Domain     *models.DomainConfig
	DNSRecords []models.DNSRecord // re-returned so customer can re-check what to add
}

// Verify polls SES for verification status and updates the DB record.
// Returns the updated domain and DNS records (re-fetched from SES) so the
// caller can display what still needs to be added.
func (s *DomainService) Verify(ctx context.Context, claims *auth.Claims, domainID uuid.UUID) (*VerifyResult, error) {
	var d *models.DomainConfig
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx dbpkg.Tx) error {
		var err error
		d, err = s.store.GetByID(ctx, tx, claims.OrgID, domainID)
		return err
	})
	if err != nil {
		return nil, err
	}

	// Poll SES for current verification state.
	attrOut, err := s.ses.GetIdentityVerificationAttributes(ctx, &ses.GetIdentityVerificationAttributesInput{
		Identities: []string{d.Domain},
	})
	if err != nil {
		return nil, fmt.Errorf("SES GetIdentityVerificationAttributes: %w", err)
	}

	attrs, ok := attrOut.VerificationAttributes[d.Domain]
	if !ok {
		return nil, fmt.Errorf("SES returned no attributes for domain %s", d.Domain)
	}

	newStatus := d.Status
	var verifiedAt *time.Time
	switch attrs.VerificationStatus {
	case types.VerificationStatusSuccess:
		now := time.Now().UTC()
		newStatus = "active"
		verifiedAt = &now
	case types.VerificationStatusFailed:
		newStatus = "failed"
	case types.VerificationStatusPending, types.VerificationStatusTemporaryFailure:
		newStatus = "verifying"
	}

	if newStatus != d.Status {
		err = dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx dbpkg.Tx) error {
			return s.store.UpdateStatus(ctx, tx, claims.OrgID, domainID, newStatus, verifiedAt)
		})
		if err != nil {
			return nil, fmt.Errorf("update domain status: %w", err)
		}
		d.Status = newStatus
		d.VerifiedAt = verifiedAt
	}

	// Re-fetch DKIM tokens so we can return current DNS records.
	dkimOut, err := s.ses.GetIdentityDkimAttributes(ctx, &ses.GetIdentityDkimAttributesInput{
		Identities: []string{d.Domain},
	})
	var dnsRecords []models.DNSRecord
	if err == nil {
		if dkimAttrs, ok := dkimOut.DkimAttributes[d.Domain]; ok {
			// Re-fetch verification token for TXT record display.
			verOut, verErr := s.ses.GetIdentityVerificationAttributes(ctx, &ses.GetIdentityVerificationAttributesInput{
				Identities: []string{d.Domain},
			})
			txtToken := ""
			if verErr == nil {
				if va, ok := verOut.VerificationAttributes[d.Domain]; ok {
					txtToken = aws.ToString(va.VerificationToken)
				}
			}
			dnsRecords = buildDNSRecords(d.Domain, txtToken, dkimAttrs.DkimTokens, s.awsRegion)
		}
	}

	return &VerifyResult{Domain: d, DNSRecords: dnsRecords}, nil
}

// Delete removes a custom domain: deletes the SES receipt rule + identity + DB record.
func (s *DomainService) Delete(ctx context.Context, claims *auth.Claims, domainID uuid.UUID) error {
	var d *models.DomainConfig
	err := dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx dbpkg.Tx) error {
		var err error
		d, err = s.store.GetByID(ctx, tx, claims.OrgID, domainID)
		return err
	})
	if err != nil {
		return err
	}

	// Remove SES receipt rule.
	ruleName := receiptRuleName(d.Domain)
	if _, err := s.ses.DeleteReceiptRule(ctx, &ses.DeleteReceiptRuleInput{
		RuleSetName: aws.String(s.ruleSetName),
		RuleName:    aws.String(ruleName),
	}); err != nil {
		slog.Warn("delete SES receipt rule", "rule", ruleName, "error", err)
		// Continue — rule may not exist if registration was partial.
	}

	// Remove SES domain identity (stops verification and DKIM).
	if _, err := s.ses.DeleteIdentity(ctx, &ses.DeleteIdentityInput{
		Identity: aws.String(d.Domain),
	}); err != nil {
		slog.Warn("delete SES identity", "domain", d.Domain, "error", err)
	}

	// Remove DB record.
	return dbpkg.WithOrgTx(ctx, s.pool, claims.OrgID, func(tx dbpkg.Tx) error {
		return s.store.Delete(ctx, tx, claims.OrgID, domainID)
	})
}

// receiptRuleName returns a safe SES receipt rule name for a domain.
// SES rule names must match [a-zA-Z0-9_-]{1,64}.
func receiptRuleName(domain string) string {
	safe := strings.NewReplacer(".", "-").Replace(domain)
	name := "custom-" + safe
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// buildDNSRecords constructs the DNS records the customer must add.
func buildDNSRecords(domain, txtToken string, dkimTokens []string, region string) []models.DNSRecord {
	records := []models.DNSRecord{
		{
			Type:    "TXT",
			Name:    "_amazonses." + domain,
			Value:   txtToken,
			Purpose: "SES domain ownership verification",
		},
		{
			Type:    "MX",
			Name:    domain,
			Value:   fmt.Sprintf("10 inbound-smtp.%s.amazonaws.com", region),
			Purpose: "Route inbound email to SES",
		},
	}
	for _, token := range dkimTokens {
		records = append(records, models.DNSRecord{
			Type:    "CNAME",
			Name:    token + "._domainkey." + domain,
			Value:   token + ".dkim.amazonses.com",
			Purpose: "DKIM email signing",
		})
	}
	return records
}

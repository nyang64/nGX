/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/golang-jwt/jwt/v5"
)

//go:embed keys/license_public.pem
var licensePublicKeyPEM []byte

var (
	licensePublicKey *ecdsa.PublicKey
	ssmOnce          sync.Once
	ssmClient        *ssm.Client

	licenseMu  sync.Mutex
	cachedJWT  string
	cachedExp  time.Time

	isColdStart = true
)

func init() {
	block, _ := pem.Decode(licensePublicKeyPEM)
	if block == nil {
		slog.Error("authorizer: failed to decode license public key PEM")
		os.Exit(1)
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		slog.Error("authorizer: failed to parse license public key", "error", err)
		os.Exit(1)
	}
	var ok bool
	licensePublicKey, ok = pub.(*ecdsa.PublicKey)
	if !ok {
		slog.Error("authorizer: license public key is not an ECDSA key")
		os.Exit(1)
	}
}

func initSSMClient(ctx context.Context) {
	ssmOnce.Do(func() {
		cfg, err := awscfg.LoadDefaultConfig(ctx)
		if err != nil {
			slog.Error("authorizer: failed to load AWS config for SSM", "error", err)
			os.Exit(1)
		}
		ssmClient = ssm.NewFromConfig(cfg)
	})
}

// LicenseClaims represents the JWT license token payload.
type LicenseClaims struct {
	jwt.RegisteredClaims
	TokenVersion int      `json:"token_version"`
	OrgID        string   `json:"org_id"`
	AWSAccountIDs []string `json:"aws_account_ids"`
	LicenseKey   string   `json:"license_key"`
	Plan         string   `json:"plan"`
	Features     []string `json:"features"`
	SeatLimit    int      `json:"seat_limit"`
	RenewalDue   int64    `json:"renewal_due"`
}

const defaultLicenseServerURL = "https://license.agent-mx.cc"
const ssmLicenseTokenPath = "/ngx/license-token"

func licenseServerURL() string {
	if v := os.Getenv("LICENSE_SERVER_URL"); v != "" {
		return v
	}
	return defaultLicenseServerURL
}

// checkLicense validates the license token and optionally revocation-checks on cold start.
func checkLicense(ctx context.Context, orgID string) (*LicenseClaims, error) {
	initSSMClient(ctx)

	licenseMu.Lock()
	defer licenseMu.Unlock()

	// Use cache if valid.
	var jwtStr string
	if cachedJWT != "" && time.Now().Before(cachedExp) {
		jwtStr = cachedJWT
	} else {
		// Fetch from SSM.
		ssmVal := ""
		out, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
			Name:           aws.String(ssmLicenseTokenPath),
			WithDecryption: aws.Bool(true),
		})
		if err == nil {
			ssmVal = aws.ToString(out.Parameter.Value)
		}

		if ssmVal == "" || ssmVal == "placeholder" {
			// No enterprise license configured — use built-in trial JWT.
			jwtStr = os.Getenv("LICENSE_TRIAL_TOKEN")
			if jwtStr == "" {
				return nil, fmt.Errorf("license: no license token available (SSM is placeholder and LICENSE_TRIAL_TOKEN is not set)")
			}
		} else {
			jwtStr = ssmVal
		}
	}

	// Parse and verify JWT.
	claims := &LicenseClaims{}
	_, err := jwt.ParseWithClaims(jwtStr, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return licensePublicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("license: invalid token: %w", err)
	}

	// Hard-deny if the license server has explicitly marked this trial as expired.
	if claims.Plan == "trial_expired" {
		return nil, fmt.Errorf("license: trial period has expired — visit license.agent-mx.cc to upgrade")
	}

	// Skip org_id check for trial plan — the global trial JWT has an empty org_id.
	if claims.Plan != "trial" && claims.OrgID != orgID {
		return nil, fmt.Errorf("license: org_id mismatch: token is for %q, request is for %q", claims.OrgID, orgID)
	}

	// Update cache with verified token.
	exp := claims.RegisteredClaims.ExpiresAt
	if exp != nil {
		cachedJWT = jwtStr
		cachedExp = exp.Time
	}

	// Cold-start revocation check.
	if isColdStart && claims.Plan != "trial" {
		if err := revocationCheck(jwtStr); err != nil {
			slog.Warn("authorizer: license revocation check failed (allowing)", "error", err)
		}
		isColdStart = false
	} else {
		isColdStart = false
	}

	// Warn if past renewal_due by more than 24h.
	if claims.RenewalDue > 0 {
		renewalDue := time.Unix(claims.RenewalDue, 0)
		if time.Now().After(renewalDue.Add(24 * time.Hour)) {
			slog.Warn("authorizer: license renewal overdue", "renewal_due", renewalDue)
		}
	}

	return claims, nil
}

// revocationCheck calls the license server's /v1/validate endpoint.
// Returns nil on success or network errors (non-blocking), returns error only on explicit invalid response.
func revocationCheck(jwtStr string) error {
	url := licenseServerURL() + "/v1/validate"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte{}))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwtStr)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Network error — log but do not deny.
		slog.Warn("authorizer: license server unreachable", "error", err)
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Valid  bool   `json:"valid"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// Can't parse response — treat as non-blocking warning.
		slog.Warn("authorizer: failed to parse license validate response", "error", err)
		return nil
	}

	if !result.Valid {
		return fmt.Errorf("license revoked: %s", result.Reason)
	}
	return nil
}

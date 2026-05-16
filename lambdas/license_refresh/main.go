/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/golang-jwt/jwt/v5"
)

const ssmBootstrapOrgIDPath = "/ngx/bootstrap-org-id"
const trialTokenPath = "/v1/trial/token"

func licenseServerURL() string {
	if v := os.Getenv("LICENSE_SERVER_URL"); v != "" {
		return v
	}
	return "https://license.agent-mx.cc"
}

func ssmTokenPath() string {
	if v := os.Getenv("SSM_LICENSE_TOKEN_PATH"); v != "" {
		return v
	}
	return "/ngx/license-token"
}

func handler(ctx context.Context) error {
	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("license_refresh: load AWS config", "error", err)
		return err
	}
	ssmClient := ssm.NewFromConfig(awsConf)

	// 1. Read current JWT from SSM.
	out, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(ssmTokenPath()),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		slog.Error("license_refresh: fetch token from SSM", "error", err)
		return err
	}
	currentJWT := aws.ToString(out.Parameter.Value)

	// 2. If SSM has placeholder, take the trial registration path.
	if currentJWT == "" || currentJWT == "placeholder" {
		return handleTrialActivation(ctx, ssmClient)
	}

	daysLeft := daysUntilExpiry(currentJWT)

	// 3. POST to license server to renew (enterprise or per-org trial).
	renewURL := licenseServerURL() + "/v1/token/renew"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, renewURL, strings.NewReader(""))
	if err != nil {
		slog.Error("license_refresh: build renew request", "error", err)
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+currentJWT)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		slog.Error("license_refresh: renew request failed", "error", err, "days_until_expiry", daysLeft)
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// 4. Handle trial expired (402) — write the signed trial_expired JWT to SSM.
	if resp.StatusCode == http.StatusPaymentRequired {
		var expResp struct {
			TrialExpired bool   `json:"trial_expired"`
			Token        string `json:"token"`
		}
		if err := json.Unmarshal(body, &expResp); err != nil || !expResp.TrialExpired || expResp.Token == "" {
			slog.Error("license_refresh: trial expired but could not parse expired token", "body", string(body))
			return fmt.Errorf("trial expired but no valid expired token in response")
		}
		if err := writeSSM(ctx, ssmClient, expResp.Token); err != nil {
			slog.Error("license_refresh: write trial_expired token to SSM", "error", err)
			return err
		}
		slog.Warn("license_refresh: trial has expired — trial_expired sentinel written to SSM")
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("license server returned %d: %s", resp.StatusCode, string(body))
		slog.Error("license_refresh: renew failed", "error", err, "days_until_expiry", daysLeft)
		return err
	}

	// 5. Parse successful renewal response.
	var renewResp struct {
		Token      string `json:"token"`
		ExpiresAt  string `json:"expires_at"`
		RenewalDue string `json:"renewal_due"`
	}
	if err := json.Unmarshal(body, &renewResp); err != nil || renewResp.Token == "" {
		slog.Error("license_refresh: parse renew response", "error", err)
		return fmt.Errorf("empty or invalid token in renew response")
	}

	// 6. Write new JWT to SSM.
	if err := writeSSM(ctx, ssmClient, renewResp.Token); err != nil {
		slog.Error("license_refresh: write renewed token to SSM", "error", err)
		return err
	}

	slog.Info("license token renewed",
		"expires_at", renewResp.ExpiresAt,
		"renewal_due", renewResp.RenewalDue,
		"days_until_expiry", daysLeft,
	)
	return nil
}

// handleTrialActivation calls POST /v1/trial/token using the bootstrap org_id from SSM.
func handleTrialActivation(ctx context.Context, ssmClient *ssm.Client) error {
	// Read org_id from /ngx/bootstrap-org-id SSM param.
	orgOut, err := ssmClient.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(ssmBootstrapOrgIDPath),
		WithDecryption: aws.Bool(false),
	})
	if err != nil {
		slog.Warn("license_refresh: bootstrap org_id not found in SSM — trial not yet registered", "error", err)
		return nil // non-fatal: setup-env.sh hasn't bootstrapped yet
	}
	orgID := aws.ToString(orgOut.Parameter.Value)
	if orgID == "" {
		slog.Warn("license_refresh: bootstrap org_id is empty — skipping trial activation")
		return nil
	}

	trialURL := licenseServerURL() + trialTokenPath
	reqBody := fmt.Sprintf(`{"org_id":%q}`, orgID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, trialURL,
		strings.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("build trial token request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		slog.Error("license_refresh: trial token request failed", "error", err)
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusPaymentRequired {
		var expResp struct {
			TrialExpired bool   `json:"trial_expired"`
			Token        string `json:"token"`
		}
		if err := json.Unmarshal(body, &expResp); err == nil && expResp.TrialExpired && expResp.Token != "" {
			if err := writeSSM(ctx, ssmClient, expResp.Token); err != nil {
				return fmt.Errorf("write trial_expired to SSM: %w", err)
			}
			slog.Warn("license_refresh: trial already expired — sentinel written to SSM")
			return nil
		}
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("trial token request returned %d: %s", resp.StatusCode, string(body))
	}

	var trialResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &trialResp); err != nil || trialResp.Token == "" {
		return fmt.Errorf("invalid trial token response: %s", string(body))
	}

	if err := writeSSM(ctx, ssmClient, trialResp.Token); err != nil {
		return fmt.Errorf("write trial token to SSM: %w", err)
	}
	slog.Info("license_refresh: trial token written to SSM", "org_id", orgID)
	return nil
}

// writeSSM writes a value to the license token SSM parameter.
func writeSSM(ctx context.Context, ssmClient *ssm.Client, value string) error {
	_, err := ssmClient.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(ssmTokenPath()),
		Value:     aws.String(value),
		Type:      ssmtypes.ParameterTypeSecureString,
		Overwrite: aws.Bool(true),
	})
	return err
}

// daysUntilExpiry parses the JWT without verification to extract the exp claim.
func daysUntilExpiry(jwtStr string) float64 {
	parser := jwt.NewParser()
	claims := jwt.MapClaims{}
	_, _, err := parser.ParseUnverified(jwtStr, claims)
	if err != nil {
		return -1
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return -1
	}
	return time.Until(exp.Time).Hours() / 24
}

func main() {
	lambda.Start(handler)
}

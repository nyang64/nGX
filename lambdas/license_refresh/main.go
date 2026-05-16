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

	// Parse exp without verification for logging.
	daysUntilExpiry := daysUntilExpiry(currentJWT)

	// 2. POST to license server to renew.
	renewURL := licenseServerURL() + "/v1/token/renew"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, renewURL, strings.NewReader(""))
	if err != nil {
		slog.Error("license_refresh: build renew request", "error", err, "days_until_expiry", daysUntilExpiry)
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+currentJWT)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		slog.Error("license_refresh: renew request failed", "error", err, "days_until_expiry", daysUntilExpiry)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("license server returned %d: %s", resp.StatusCode, string(body))
		slog.Error("license_refresh: renew failed", "error", err, "days_until_expiry", daysUntilExpiry)
		return err
	}

	// 3. Parse response.
	var renewResp struct {
		Token      string `json:"token"`
		ExpiresAt  string `json:"expires_at"`
		RenewalDue string `json:"renewal_due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&renewResp); err != nil {
		slog.Error("license_refresh: parse renew response", "error", err, "days_until_expiry", daysUntilExpiry)
		return err
	}
	if renewResp.Token == "" {
		err := fmt.Errorf("license server returned empty token")
		slog.Error("license_refresh: empty token in response", "error", err, "days_until_expiry", daysUntilExpiry)
		return err
	}

	// 4. Write new JWT to SSM.
	overwrite := true
	_, err = ssmClient.PutParameter(ctx, &ssm.PutParameterInput{
		Name:      aws.String(ssmTokenPath()),
		Value:     aws.String(renewResp.Token),
		Type:      ssmtypes.ParameterTypeSecureString,
		Overwrite: aws.Bool(overwrite),
	})
	if err != nil {
		slog.Error("license_refresh: write renewed token to SSM", "error", err, "days_until_expiry", daysUntilExpiry)
		return err
	}

	slog.Info("license token renewed",
		"expires_at", renewResp.ExpiresAt,
		"renewal_due", renewResp.RenewalDue,
	)
	return nil
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

/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	sessdk "github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/jackc/pgx/v5/pgxpool"

	"agentmail/lambdas/shared"
	"agentmail/pkg/models"
	domsvc "agentmail/services/domains/service"
	domstore "agentmail/services/domains/store"
)

var (
	pool   *pgxpool.Pool
	domSvc *domsvc.DomainService
)

func init() {
	ctx := context.Background()
	pool = shared.InitDB()

	awsConf, err := awscfg.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("domains: load AWS config", "error", err)
		os.Exit(1)
	}
	sesClient := sessdk.NewFromConfig(awsConf)
	domSt := domstore.NewPostgresDomainStore(pool)

	domSvc = domsvc.New(
		pool,
		domSt,
		sesClient,
		os.Getenv("SES_RULE_SET_NAME"),
		os.Getenv("S3_BUCKET_EMAILS"),
		os.Getenv("AWS_REGION"),
	)
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	claims, err := shared.ExtractClaims(event)
	if err != nil {
		return shared.Error(401, "unauthorized"), nil
	}

	switch event.Resource {
	case "/v1/domains":
		switch event.HTTPMethod {
		case "GET":
			domains, err := domSvc.List(ctx, claims)
			if err != nil {
				return shared.Error(500, err.Error()), nil
			}
			if domains == nil {
				domains = []*models.DomainConfig{}
			}
			return shared.JSON(200, map[string]any{"domains": domains}), nil
		case "POST":
			var req domsvc.RegisterRequest
			if err := shared.Decode(event, &req); err != nil {
				return shared.Error(400, "invalid request body"), nil
			}
			result, err := domSvc.Register(ctx, claims, req)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(201, result), nil
		}

	case "/v1/domains/{domain_id}":
		domainID, err := shared.ParseUUID(shared.PathParam(event, "domain_id"))
		if err != nil {
			return shared.Error(400, "invalid domain ID"), nil
		}
		switch event.HTTPMethod {
		case "GET":
			domain, err := domSvc.Get(ctx, claims, domainID)
			if err != nil {
				return shared.Error(404, "domain not found"), nil
			}
			return shared.JSON(200, domain), nil
		case "DELETE":
			if err := domSvc.Delete(ctx, claims, domainID); err != nil {
				return shared.Error(404, "domain not found"), nil
			}
			return events.APIGatewayProxyResponse{StatusCode: 204}, nil
		}

	case "/v1/domains/{domain_id}/verify":
		if event.HTTPMethod == "POST" {
			domainID, err := shared.ParseUUID(shared.PathParam(event, "domain_id"))
			if err != nil {
				return shared.Error(400, "invalid domain ID"), nil
			}
			result, err := domSvc.Verify(ctx, claims, domainID)
			if err != nil {
				return shared.Error(400, err.Error()), nil
			}
			return shared.JSON(200, result), nil
		}
	}

	return shared.Error(404, "not found"), nil
}

func main() {
	lambda.Start(handler)
}

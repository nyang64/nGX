/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

// bootstrap creates the first organization and an admin API key directly in the
// database. Run this once after migrations to get a key you can use with the API.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./tools/bootstrap \
//	  -org "My Org" -slug "my-org"
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const keyPrefix = "am_live_"

func main() {
	orgName := flag.String("org", "Default Org", "Organization name")
	orgSlug := flag.String("slug", "default", "Organization slug (unique)")
	keyName := flag.String("key", "bootstrap", "API key name")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "database URL")
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("DATABASE_URL required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	// Insert org.
	orgID := uuid.New()
	_, err = pool.Exec(ctx,
		`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
		orgID, *orgName, *orgSlug,
	)
	if err != nil {
		log.Fatalf("create organization: %v", err)
	}

	// Generate API key.
	plaintext, hash, prefix, err := generateKey()
	if err != nil {
		log.Fatalf("generate api key: %v", err)
	}

	allScopes := []string{
		"org:admin", "pod:admin",
		"inbox:read", "inbox:write",
		"webhook:read", "webhook:write",
		"draft:read", "draft:write",
		"search:read",
	}

	keyID := uuid.New()
	_, err = pool.Exec(ctx,
		`INSERT INTO api_keys (id, org_id, name, key_prefix, key_hash, scopes, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		keyID, orgID, *keyName, prefix, hash, allScopes, time.Now().UTC(),
	)
	if err != nil {
		log.Fatalf("create api key: %v", err)
	}

	fmt.Println("Bootstrap complete.")
	fmt.Printf("  Org ID:  %s\n", orgID)
	fmt.Printf("  Org:     %s (slug: %s)\n", *orgName, *orgSlug)
	fmt.Printf("  API Key: %s\n", plaintext)
	fmt.Println()
	fmt.Println("Save the API key — it will not be shown again.")
	fmt.Printf("  export KEY=%s\n", plaintext)
}

func generateKey() (plaintext, hash, displayPrefix string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate random bytes: %w", err)
	}
	plaintext = keyPrefix + base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	if len(plaintext) >= 16 {
		displayPrefix = plaintext[:16]
	} else {
		displayPrefix = plaintext
	}
	return plaintext, hash, displayPrefix, nil
}

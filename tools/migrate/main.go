/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	var (
		dbURL     = flag.String("db", os.Getenv("DATABASE_URL"), "database URL")
		direction = flag.String("cmd", "up", "migration command: up, down, version, force")
		steps     = flag.Int("steps", 0, "number of steps (for down)")
		migsPath  = flag.String("path", "file://migrations", "path to migrations")
	)
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("DATABASE_URL required")
	}

	m, err := migrate.New(*migsPath, *dbURL)
	if err != nil {
		log.Fatalf("create migrate: %v", err)
	}
	defer m.Close()

	switch *direction {
	case "up":
		if err := m.Up(); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("migrate up: %v", err)
		}
		fmt.Println("migrations applied")
	case "down":
		n := *steps
		if n == 0 {
			n = 1
		}
		if err := m.Steps(-n); err != nil && err != migrate.ErrNoChange {
			log.Fatalf("migrate down: %v", err)
		}
		fmt.Printf("rolled back %d migration(s)\n", n)
	case "version":
		v, dirty, err := m.Version()
		if err != nil {
			log.Fatalf("version: %v", err)
		}
		fmt.Printf("version: %d, dirty: %v\n", v, dirty)
	case "force":
		v := *steps
		if err := m.Force(v); err != nil {
			log.Fatalf("force: %v", err)
		}
		fmt.Printf("forced version %d\n", v)
	default:
		log.Fatalf("unknown command: %s", *direction)
	}
}

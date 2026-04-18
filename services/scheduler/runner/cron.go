/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package runner

import (
	"context"
	"log/slog"

	"github.com/robfig/cron/v3"
)

// Cron wraps robfig/cron with a context-aware job API.
type Cron struct {
	c *cron.Cron
}

// New creates a Cron that supports second-level precision.
func New() *Cron {
	return &Cron{c: cron.New(cron.WithSeconds())}
}

// Add schedules job to run according to spec (6-field cron expression with seconds).
func (cr *Cron) Add(spec string, name string, job func(ctx context.Context)) {
	_, err := cr.c.AddFunc(spec, func() {
		slog.Debug("cron job running", "job", name)
		job(context.Background())
	})
	if err != nil {
		slog.Error("failed to register cron job", "job", name, "spec", spec, "error", err)
	}
}

// Start begins the scheduler in a background goroutine.
func (cr *Cron) Start() { cr.c.Start() }

// Stop halts the scheduler and waits for any running jobs to complete.
func (cr *Cron) Stop() { cr.c.Stop() }

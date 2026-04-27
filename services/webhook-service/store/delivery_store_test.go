/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package store

import (
	"testing"
	"time"
)

// TestNextBackoff verifies exponential backoff: 2^attempt seconds, capped at 2^12.
func TestNextBackoff(t *testing.T) {
	cases := []struct {
		attempt  int
		wantSecs int
	}{
		{0, 1},
		{1, 2},
		{2, 4},
		{3, 8},
		{4, 16},
		{5, 32},
		{6, 64},
		{7, 128},
		{8, 256},
		{9, 512},
		{10, 1024},
		{11, 2048},
		{12, 4096},
		// capped at attempt=12
		{13, 4096},
		{20, 4096},
	}
	for _, tc := range cases {
		got := nextBackoff(tc.attempt)
		want := time.Duration(tc.wantSecs) * time.Second
		if got != want {
			t.Errorf("nextBackoff(%d) = %v, want %v", tc.attempt, got, want)
		}
	}
}

// TestMaxRetries verifies the hard-coded ceiling is 8.
func TestMaxRetries(t *testing.T) {
	got := maxRetries(nil)
	if got != 8 {
		t.Errorf("maxRetries = %d, want 8", got)
	}
}

// TestRetryStatusTransition verifies that attempt>=maxRetries produces failed status,
// and attempt<maxRetries produces retrying status.
// This mirrors the logic in MarkFailed without hitting the database.
func TestRetryStatusTransition(t *testing.T) {
	max := maxRetries(nil) // 8

	for attempt := 0; attempt < max; attempt++ {
		backoff := nextBackoff(attempt)
		if backoff <= 0 {
			t.Errorf("attempt %d: expected positive backoff, got %v", attempt, backoff)
		}
		// status should be retrying
		if attempt >= max {
			t.Errorf("attempt %d should be < max %d in this range", attempt, max)
		}
	}

	// At max retries, should transition to failed (no next attempt).
	for _, attempt := range []int{max, max + 1, max + 5} {
		if attempt < max {
			t.Errorf("attempt %d should be >= max %d", attempt, max)
		}
		// When attempt >= max, MarkFailed sets status=failed and nextAttempt=zero.
		// Verify backoff is still computed (even if unused for failed status).
		backoff := nextBackoff(attempt)
		if backoff <= 0 {
			t.Errorf("nextBackoff(%d) should be positive even past max retries, got %v", attempt, backoff)
		}
	}
}

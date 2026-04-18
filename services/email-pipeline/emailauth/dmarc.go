/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package emailauth

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/emersion/go-msgauth/dmarc"
)

// CheckDMARC fetches the DMARC policy for fromDomain and decides the disposition
// based on the SPF and DKIM results.
//
// Alignment is simplified: DMARC passes if either SPF or DKIM passed, regardless
// of strict/relaxed domain alignment. This is sufficient for the common case;
// full RFC 7489 alignment can be layered on later.
func CheckDMARC(fromDomain string, spfResult SPFResult, dkimResult DKIMResult) DMARCDisposition {
	record, err := dmarc.Lookup(fromDomain)
	if err != nil {
		if errors.Is(err, dmarc.ErrNoPolicy) {
			// No DMARC record — nothing to enforce.
			return DMARCNone
		}
		if dmarc.IsTempFail(err) {
			slog.Warn("DMARC lookup temp failure", "domain", fromDomain, "error", err)
			return DMARCNone // fail open on transient DNS errors
		}
		slog.Warn("DMARC lookup error", "domain", fromDomain, "error", err)
		return DMARCNone
	}

	// DMARC passes if at least one authentication mechanism passed.
	spfPassed := spfResult == SPFPass || spfResult == SPFNeutral
	dkimPassed := dkimResult == DKIMPass

	if spfPassed || dkimPassed {
		return DMARCNone // authentication passed — no enforcement needed
	}

	// Both mechanisms failed; enforce the domain's policy.
	switch record.Policy {
	case dmarc.PolicyReject:
		return DMARCReject
	case dmarc.PolicyQuarantine:
		return DMARCQuarantine
	default:
		return DMARCNone
	}
}

// ExtractFromDomain returns the domain part of an RFC 5322 From address,
// e.g. "alice@example.com" → "example.com". Returns empty string on failure.
func ExtractFromDomain(from string) string {
	at := strings.LastIndex(from, "@")
	if at < 0 || at == len(from)-1 {
		return ""
	}
	return strings.ToLower(from[at+1:])
}

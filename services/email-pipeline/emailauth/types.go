/*
 * Copyright (c) 2026 nyklabs.com. All rights reserved.
 *
 * Licensed under the nGX Commercial Source License v1.0.
 * See LICENSE file in the project root for full license information.
 */

package emailauth

// SPFResult is the outcome of an SPF check.
type SPFResult string

const (
	SPFPass      SPFResult = "pass"
	SPFFail      SPFResult = "fail"
	SPFSoftFail  SPFResult = "softfail"
	SPFNeutral   SPFResult = "neutral"
	SPFNone      SPFResult = "none"
	SPFTempError SPFResult = "temperror"
	SPFPermError SPFResult = "permerror"
)

// DKIMResult is the outcome of DKIM verification.
type DKIMResult string

const (
	DKIMPass    DKIMResult = "pass"
	DKIMFail    DKIMResult = "fail"
	DKIMNone    DKIMResult = "none"
	DKIMTempErr DKIMResult = "temperror"
)

// DMARCDisposition is the action determined by DMARC policy evaluation.
type DMARCDisposition string

const (
	DMARCNone       DMARCDisposition = "none"
	DMARCQuarantine DMARCDisposition = "quarantine"
	DMARCReject     DMARCDisposition = "reject"
)

// AuthResults collects all email authentication outcomes for an inbound message.
type AuthResults struct {
	SPF   SPFResult
	DKIM  DKIMResult
	DMARC DMARCDisposition
}

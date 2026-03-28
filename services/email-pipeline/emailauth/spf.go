package emailauth

import (
	"net"

	"blitiri.com.ar/go/spf"
)

// CheckSPF verifies the SPF policy for a message by checking whether remoteIP
// is authorised to send on behalf of mailFrom. helo is the EHLO/HELO hostname
// presented by the connecting MTA.
func CheckSPF(remoteIP net.IP, helo, mailFrom string) SPFResult {
	if remoteIP == nil {
		return SPFNone
	}
	result, _ := spf.CheckHostWithSender(remoteIP, helo, mailFrom)
	switch result {
	case spf.Pass:
		return SPFPass
	case spf.Fail:
		return SPFFail
	case spf.SoftFail:
		return SPFSoftFail
	case spf.Neutral:
		return SPFNeutral
	case spf.TempError:
		return SPFTempError
	case spf.PermError:
		return SPFPermError
	default:
		return SPFNone
	}
}

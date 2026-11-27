//go:build !leafen_noapps

package app

import "strings"

// appStatusInputRefusal is why a body can't set an app's status, or "": one
// that doesn't read, or that leaves status out, which it names
// (api_missing_input.go); a status that isn't one is said where it is set
// (appInstallStatusRefusal).
func appStatusInputRefusal(decodeErr error, input appStatusInput) string {
	switch {
	case decodeErr != nil:
		return bodyRefusalReason("app status", decodeErr)
	case strings.TrimSpace(input.Status) == "":
		return "status is empty: give active, stopped or emergency"
	}
	return ""
}

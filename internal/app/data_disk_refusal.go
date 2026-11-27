package app

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
)

// A write refused for the disk (data_disk_floor.go) says which of two it
// is, and to whom (2026-11-27: a hosting platform's 0.5 GB trial volume,
// smaller than the floor then was, read "almost full, ask the server's
// administrator to free space" to the administrator who had just set the
// server up): the disk too full, which freeing space mends, or too small
// for the write, which only a larger disk mends. An administrator of the
// server is told what to do — on Railway, which the server knows it runs on
// (setup_hosting_platform.go), to grow its volume there; anyone else is told
// whom to ask. The refusal's cause goes beside its code
// (api_error_because.go) for the screens to say in the person's language,
// and its message says the same in English for an agent or another client.

var serverDiskRefusalMessages = map[string]string{
	serverDiskRefusalFull: "The server's disk is almost full. Ask the server's administrator to free space; " +
		"this works again once there is room.",
	serverDiskRefusalSmall: "The server's disk is too small to hold this. Ask the server's administrator for a larger disk.",
	serverDiskRefusalFullAdministrator: "The server's disk is almost full. Free space on it, or move the data folder " +
		"to a larger disk; this works again once there is room.",
	serverDiskRefusalSmallAdministrator: "The server's disk is too small to hold this. Move the data folder to a larger disk.",
	serverDiskRefusalFullRailway: "The server's Railway volume is almost full. Grow it in Railway: open the volume " +
		"and choose Live Resize (on the Hobby and Pro plans; a Trial or Free plan's volume stays at 0.5 GB). " +
		"This works again once there is room.",
	serverDiskRefusalSmallRailway: "The server's Railway volume is too small to hold this. Grow it in Railway: open " +
		"the volume and choose Live Resize (on the Hobby and Pro plans; a Trial or Free plan's volume stays at 0.5 GB).",
}

// serverDiskRefusalCause is how a write refused for the disk (refusal, from
// requireDataDiskRoom) is said to whoever asked, as one of the
// serverDiskRefusal causes.
func serverDiskRefusalCause(event *core.RequestEvent, refusal error) string {
	small := errors.Is(refusal, errServerDiskSmall)
	railway := setupHostingPlatform() == setupPlatformRailway
	switch administers := serverDiskAskerAdministers(event); {
	case !administers && small:
		return serverDiskRefusalSmall
	case !administers:
		return serverDiskRefusalFull
	case railway && small:
		return serverDiskRefusalSmallRailway
	case railway:
		return serverDiskRefusalFullRailway
	case small:
		return serverDiskRefusalSmallAdministrator
	}
	return serverDiskRefusalFullAdministrator
}

// serverDiskRefusalMessage is the refusal's message to whoever asked.
func serverDiskRefusalMessage(event *core.RequestEvent, refusal error) string {
	return serverDiskRefusalMessages[serverDiskRefusalCause(event, refusal)]
}

// serverDiskAskerAdministers says whether the request's account administers
// the server.
func serverDiskAskerAdministers(event *core.RequestEvent) bool {
	if event == nil || event.Auth == nil {
		return false
	}
	administrator, err := isAdministrator(event.App, event.Auth.Id)
	return err == nil && administrator
}

// serverDiskRetryAfter says when to look again: a full disk may have room in
// a while; a disk too small for the write has none until it is replaced.
func serverDiskRetryAfter(event *core.RequestEvent, refusal error) {
	if !errors.Is(refusal, errServerDiskSmall) {
		event.Response.Header().Set("Retry-After", strconv.Itoa(dataDiskRetryAfter))
	}
}

// writeServerDiskLow answers a write refused for the disk: 507, the reason
// in the asker's words, and when to look again.
func writeServerDiskLow(event *core.RequestEvent, operation string, refusal error) error {
	serverDiskRetryAfter(event, refusal)
	cause := serverDiskRefusalCause(event, refusal)
	apiErrorBecause(event, "", cause)
	return apiErrorWithCause(event, http.StatusInsufficientStorage, errorCodeServerDiskLow,
		serverDiskRefusalMessages[cause], operation, "disk-floor", errorCodeServerDiskLow, refusal)
}

package app

import (
	"net/url"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// pairAddress is the other server of this one's standby pair, which the
// apps that know it look at when this server stops answering (the pair's
// addresses, follow-up of 2026-10-17): a standby's primary, or a primary's
// standby by the address its beat carries. The apps go there only once it
// says it serves as the primary. An address that is this server's own isn't
// one: a Tailscale address or a tunnel follows the data to the server that
// took over anyway.
func pairAddress(app core.App, stored storedScheduledBackup, loaded bool) string {
	other := ""
	runner, running := app.Store().Get(replicationRunnerStoreKey).(*replicationRunner)
	switch {
	case loaded && stored.StandbyEnabled:
		other = stored.StandbyPrimaryOrigin
		if running {
			other = firstNonEmpty(runner.standbyStatus().PrimaryOrigin, other)
		}
	case running:
		if standby, _, _ := runner.lease.snapshot(time.Now()); standby != nil {
			other = standby.Origin
		}
	}
	origin := webOrigin(other)
	if origin == "" || origin == webOrigin(configuredPublicOrigin()) {
		return ""
	}
	return origin
}

// webOrigin is an address's http(s) origin, "" for anything else.
func webOrigin(address string) string {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}

// keepStandbyPrimaryOriginBehind keeps it behind the beat, stopped with the
// server (server_background.go).
func keepStandbyPrimaryOriginBehind(app core.App, previous, current string) {
	if app != nil {
		serverBackgroundOf(app).behind("standby primary address", func() { keepStandbyPrimaryOrigin(app, previous, current) })
	}
}

// keepStandbyPrimaryOrigin writes down the primary's address its beat gave,
// in place of the one this standby was set up with.
func keepStandbyPrimaryOrigin(app core.App, previous, current string) {
	if app == nil {
		return
	}
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_scheduled_backups}} SET [[standby_primary_origin]] = {:current}
		WHERE [[standby_enabled]] = 1 AND [[standby_primary_origin]] = {:previous}`).
		Bind(dbx.Params{"current": current, "previous": previous}).Execute(); err != nil {
		app.Logger().Warn("standby: couldn't keep the primary's new address", "error", err)
	}
}

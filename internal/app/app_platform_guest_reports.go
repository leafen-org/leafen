//go:build !leafen_noapps

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Reporting a guest (docs/app-platform.md 9): someone using the app reports
// a guest in their organization's room by the ID Leafen gives the app with
// the guest's messages; the report is kept for the administrator. Blocking
// ends the guest's session and its sockets, and keeps the same address out
// of that link while it lasts — the link's other guests stay.

const appGuestReportReasonRunes = 500

// appGuestAddressHash names a guest's address for one link only.
func appGuestAddressHash(linkID, address string) string {
	digest := sha256.Sum256([]byte("leafen-app-guest-address\x00" + linkID + "\x00" + address))
	return hex.EncodeToString(digest[:16])
}

func appGuestBlocked(app core.App, installID, linkID, addressHash string) bool {
	var count int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_app_guest_blocks}} WHERE [[install_id]] = {:install} AND [[link_id]] = {:link}
		AND [[address_hash]] = {:address}`).Bind(dbx.Params{"install": installID, "link": linkID, "address": addressHash}).Row(&count)
	return err == nil && count > 0
}

func reportAppGuestRoute(event *core.RequestEvent) error {
	install, _, personID, ok, failure := appTokenInstall(event)
	if !ok {
		return failure
	}
	authority, _ := userKeyOf(event)
	input := appGuestReportInput{}
	if err := json.NewDecoder(io.LimitReader(event.Request.Body, appPlatformInputMaxBytes)).Decode(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeAppGuestRefused,
			refusalMessage("Say why in up to 500 characters.", bodyRefusalReason("guest report", err)))
	}
	if count := utf8.RuneCountInString(input.Reason); count > appGuestReportReasonRunes {
		return apiError(event, http.StatusBadRequest, errorCodeAppGuestRefused,
			refusalMessage("Say why in up to 500 characters.", fmt.Sprintf("reason is %d characters", count)))
	}
	guestID := event.Request.PathValue("guestId")
	var token string
	var session appGuestSession
	appGuests.mu.Lock()
	for candidate, kept := range appGuests.sessions {
		if kept.InstallID == install.ID && kept.Tenancy == authority.TenancyKey && appGuestTokenHash(candidate)[:16] == guestID {
			token, session = candidate, kept
		}
	}
	if token != "" && input.Block {
		delete(appGuests.sessions, token)
	}
	appGuests.mu.Unlock()
	if token == "" {
		return apiError(event, http.StatusNotFound, errorCodeAppGuestRefused, "No such guest is here.")
	}
	now := time.Now().UnixMilli()
	err := event.App.RunInTransaction(func(tx core.App) error {
		blocked := 0
		if input.Block {
			blocked = 1
			if _, err := tx.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_app_guest_blocks}} ([[install_id]], [[link_id]], [[address_hash]], [[created]])
				VALUES ({:install}, {:link}, {:address}, {:now})`).Bind(dbx.Params{"install": install.ID, "link": session.LinkID,
				"address": session.AddressHash, "now": now}).Execute(); err != nil {
				return err
			}
		}
		if _, err := tx.DB().Insert("leafen_app_guest_reports", dbx.Params{
			"id": security.RandomString(16), "install_id": install.ID, "tenancy": session.Tenancy, "link_id": session.LinkID, "room": session.Room,
			"guest_id": guestID, "reporter_id": personID, "reason": strings.TrimSpace(input.Reason), "blocked": blocked, "created": now,
		}).Execute(); err != nil {
			return err
		}
		return recordAppAudit(tx, install, "guest-report", personID, map[string]any{"link": session.LinkID, "room": session.Room, "blocked": input.Block})
	})
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if input.Block {
		appRoomRegistry.closeGuest(install.ID, "guest:"+guestID)
	}
	return event.JSON(http.StatusOK, appGuestReportResult{Blocked: input.Block})
}

// loadAppGuestReports is a window of the reports, newest first, from after
// on, for the administrator.
func loadAppGuestReports(app core.App, installID string, after timeIDCursor) ([]appGuestReportWire, string, error) {
	type reportRow struct {
		ID       string `db:"id"`
		Created  int64  `db:"created"`
		Room     string `db:"room"`
		LinkID   string `db:"link_id"`
		Reporter string `db:"reporter"`
		Reason   string `db:"reason"`
		Blocked  bool   `db:"blocked"`
	}
	rows := []reportRow{}
	if err := app.DB().NewQuery(`SELECT [[report]].[[id]], [[report]].[[created]], [[report]].[[room]], [[report]].[[link_id]],
			` + accountNameSQL("user") + ` AS [[reporter]], [[report]].[[reason]], [[report]].[[blocked]]
		FROM {{leafen_app_guest_reports}} [[report]] LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[report]].[[reporter_id]]
		WHERE [[report]].[[install_id]] = {:install}
			AND ({:afterId} = '' OR [[report]].[[created]] < {:afterAt} OR ([[report]].[[created]] = {:afterAt} AND [[report]].[[id]] > {:afterId}))
		ORDER BY [[report]].[[created]] DESC, [[report]].[[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"install": installID, "afterAt": after.At, "afterId": after.ID, "limit": appGuestReportsPerWindow + 1}).All(&rows); err != nil {
		return nil, "", err
	}
	rows, next := appWindowOf(rows, appGuestReportsPerWindow, func(row reportRow) (int64, string) { return row.Created, row.ID })
	reports := make([]appGuestReportWire, 0, len(rows))
	for _, row := range rows {
		reports = append(reports, appGuestReportWire{Created: row.Created, Room: row.Room, LinkID: row.LinkID, Reporter: row.Reporter,
			Reason: row.Reason, Blocked: row.Blocked})
	}
	return reports, next, nil
}

// pruneAppGuestReports lets blocks go with their links and reports after 90
// days, each table read outside the write lock (retire_rows_window.go).
func pruneAppGuestReports(app core.App, now time.Time) error {
	if err := retireAllBoundRows(app, "leafen_app_guest_blocks", appGuestBlockGoneSQL, dbx.Params{"now": now.UnixMilli()}); err != nil {
		return err
	}
	return retireAllBoundRows(app, "leafen_app_guest_reports", func(alias string) string {
		return alias + `.[[created]] < {:before}`
	}, dbx.Params{"before": now.Add(-90 * 24 * time.Hour).UnixMilli()})
}

// appGuestBlockGoneSQL holds for the aliased block when its link is gone,
// over or taken back.
func appGuestBlockGoneSQL(alias string) string {
	return `NOT EXISTS (SELECT 1 FROM {{leafen_app_guest_links}} AS [[link]]
		WHERE [[link]].[[id]] = ` + alias + `.[[link_id]] AND [[link]].[[expires_at]] > {:now} AND [[link]].[[revoked_at]] = 0)`
}

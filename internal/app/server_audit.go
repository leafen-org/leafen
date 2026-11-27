package app

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The server audit log shows server administrators, newest first and a
// window at a time, what administrators did to accounts, which server
// settings they changed and every sign-in attempt. Secrets never enter it:
// a changed credential is recorded as a change to its section.

const administratorAuditCursorPrefix = "haud1."

type storedAdministratorAuditEvent struct {
	Kind         string `db:"kind"`
	SortKey      string `db:"sort_key"`
	ID           string `db:"id"`
	OccurredAt   int64  `db:"occurred_at"`
	Action       string `db:"action"`
	ActorID      string `db:"actor_id"`
	TargetID     string `db:"target_id"`
	TargetMail   string `db:"target_email"`
	FromValue    string `db:"from_value"`
	ToValue      string `db:"to_value"`
	Method       string `db:"method"`
	Phase        string `db:"phase"`
	Outcome      string `db:"outcome"`
	ReasonCode   string `db:"reason_code"`
	ReasonStatus int    `db:"reason_status"`
	IP           string `db:"ip"`
	UserAgent    string `db:"user_agent"`
}

func registerServerAuditRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/admin/audit", listAdministratorAuditLog)
}

// recordAdministratorAccountAudit keeps an administrator action on an account.
func recordAdministratorAccountAudit(
	app core.App, actorID, targetID, action, fromStatus, toStatus string, revision int, requestID string, at int64,
) error {
	_, err := app.DB().Insert(administratorAccountAuditTable, dbx.Params{
		"id": core.GenerateDefaultRandomId(), "actor_id": actorID, "target_user_id": targetID,
		"action": action, "from_status": fromStatus, "to_status": toStatus,
		"revision": revision, "request_id": requestID, "created_at": at,
	}).Execute()
	return err
}

// recordServerSettingsAudit keeps one event per server setting a save
// changed. Sections holding credentials record only whether they are on.
func recordServerSettingsAudit(
	app core.App, actorID string, revision int, current, next storedAdministratorSettings, at int64,
) error {
	type change struct {
		field, from, to string
		changed         bool
	}
	on := strconv.FormatBool
	changes := []change{
		{"signup_policy", current.SignupPolicy, next.SignupPolicy, current.SignupPolicy != next.SignupPolicy},
		{"signup_contact", current.SignupContact, next.SignupContact, current.SignupContact != next.SignupContact},
		{"server_name", current.ServerName, next.ServerName, current.ServerName != next.ServerName},
		{"member_add_policy", current.MemberAddPolicy, next.MemberAddPolicy, current.MemberAddPolicy != next.MemberAddPolicy},
		{"require_email_verification", on(current.RequireEmailVerification), on(next.RequireEmailVerification),
			current.RequireEmailVerification != next.RequireEmailVerification},
		{"smtp", on(current.SMTPEnabled), on(next.SMTPEnabled), current.SMTPEnabled != next.SMTPEnabled ||
			current.SMTPHost != next.SMTPHost || current.SMTPPort != next.SMTPPort || current.SMTPUsername != next.SMTPUsername ||
			current.SMTPPasswordCipher != next.SMTPPasswordCipher || current.SMTPTLSMode != next.SMTPTLSMode ||
			current.SenderName != next.SenderName || current.SenderAddress != next.SenderAddress},
		{"google", on(current.GoogleEnabled), on(next.GoogleEnabled), current.GoogleEnabled != next.GoogleEnabled ||
			current.GoogleClientID != next.GoogleClientID || current.GoogleSecretCipher != next.GoogleSecretCipher},
		{"oidc", on(current.OIDCEnabled), on(next.OIDCEnabled), current.OIDCEnabled != next.OIDCEnabled ||
			current.OIDCName != next.OIDCName || current.OIDCIssuer != next.OIDCIssuer || current.OIDCClientID != next.OIDCClientID ||
			current.OIDCSecretCipher != next.OIDCSecretCipher || current.OIDCScopes != next.OIDCScopes},
		{"place_search", on(current.PlaceSearchEnabled), on(next.PlaceSearchEnabled), current.PlaceSearchEnabled != next.PlaceSearchEnabled || current.PlaceSearchConfigCipher != next.PlaceSearchConfigCipher},
		{"map", on(current.MapTilesEnabled), on(next.MapTilesEnabled), current.MapTilesEnabled != next.MapTilesEnabled ||
			current.MapTileURL != next.MapTileURL || current.MapAttribution != next.MapAttribution ||
			current.MapAttributionURL != next.MapAttributionURL},
		{"update_check", on(current.UpdateCheckEnabled), on(next.UpdateCheckEnabled), current.UpdateCheckEnabled != next.UpdateCheckEnabled},
		{"switcher_lists", on(current.SwitcherListsEnabled), on(next.SwitcherListsEnabled),
			current.SwitcherListsEnabled != next.SwitcherListsEnabled},
		{"read_log", on(current.ReadLogEnabled), on(next.ReadLogEnabled), current.ReadLogEnabled != next.ReadLogEnabled},
	}
	for _, item := range changes {
		if !item.changed {
			continue
		}
		if _, err := app.DB().Insert("leafen_server_settings_audit_events", dbx.Params{
			"id": core.GenerateDefaultRandomId(), "actor_id": actorID, "revision": revision, "field": item.field,
			"from_value": item.from, "to_value": item.to, "created_at": at,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func listAdministratorAuditLog(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	query := event.Request.URL.Query()
	kind := strings.TrimSpace(query.Get("kind"))
	if kind != "" && kind != administratorAuditKindAccount && kind != administratorAuditKindSettings &&
		kind != administratorAuditKindSignIn {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the audit log type.")
	}
	at, key, ok := decodeAdministratorAuditCursor(strings.TrimSpace(query.Get("cursor")))
	if !ok {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the list position.")
	}
	rows, err := queryAdministratorAuditEvents(event.App, kind, at, key, administratorAuditLimit+1)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed,
			"Couldn't read the audit log.", "administrator-audit", "read", errorCodeAdministratorReadFailed, err)
	}
	log := administratorAuditLog{Items: []administratorAuditEvent{}}
	if len(rows) > administratorAuditLimit {
		rows = rows[:administratorAuditLimit]
		last := rows[len(rows)-1]
		cursor := administratorAuditCursorPrefix + base64.RawURLEncoding.EncodeToString(
			[]byte(strconv.FormatInt(last.OccurredAt, 10)+"|"+last.SortKey))
		log.NextCursor = &cursor
	}
	people, err := loadAdministratorAuditPeople(event.App, rows)
	if err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAdministratorReadFailed,
			"Couldn't read the audit log.", "administrator-audit", "people", errorCodeAdministratorReadFailed, err)
	}
	for _, row := range rows {
		actor, target := people[row.ActorID], people[row.TargetID]
		// A line names an account as it stands, and a deleted one by no one
		// but on its deletion line, which keeps the email it had then
		// (deletionAuditEmail; user decision 2026-11-07, 2026-11-15: its
		// sign-in lines still showed it). A sign-in attempt that named no
		// account shows the address as it was typed.
		if row.Kind == administratorAuditKindSignIn && row.TargetID == "" ||
			row.Kind == administratorAuditKindAccount && visibleAccountEmail(target.Email) == "" && row.TargetMail != "" {
			target.Email = row.TargetMail
		}
		// Someone who deleted their own account is the line's actor too,
		// named by the same email (2026-11-13: it read "알 수 없는 사용자").
		if row.ActorID != "" && row.ActorID == row.TargetID {
			actor = target
		}
		actor.Email, target.Email = visibleAccountEmail(actor.Email), visibleAccountEmail(target.Email)
		item := administratorAuditEvent{
			ID: row.ID, Kind: row.Kind, Action: row.Action,
			ActorID: row.ActorID, ActorName: actor.Name, ActorEmail: actor.Email,
			TargetID: row.TargetID, TargetName: target.Name, TargetEmail: target.Email,
			FromValue: row.FromValue, ToValue: row.ToValue, Method: row.Method, Phase: row.Phase, Outcome: row.Outcome,
			ReasonCode: row.ReasonCode, ReasonStatus: row.ReasonStatus, IP: row.IP, UserAgent: row.UserAgent,
			OccurredAt: time.UnixMilli(row.OccurredAt).UTC().Format(time.RFC3339Nano),
		}
		log.Items = append(log.Items, item)
	}
	return event.JSON(http.StatusOK, log)
}

// queryAdministratorAuditEvents reads the next window of each source
// through its time index and merges them newest first.
//
// Sign-in events keep their time in nanoseconds and the others in
// milliseconds: the list reads every time in milliseconds, and a position
// in it (a millisecond and a key) covers the sign-in events of that
// millisecond through a range of their own times, so their index serves.
func queryAdministratorAuditEvents(app core.App, kind string, at int64, key string, limit int) ([]storedAdministratorAuditEvent, error) {
	wants := func(value string) bool { return kind == "" || kind == value }
	atNanos, atNanosNext := int64(math.MaxInt64), int64(math.MaxInt64)
	if at < math.MaxInt64/int64(time.Millisecond)-1 {
		atNanos, atNanosNext = at*int64(time.Millisecond), (at+1)*int64(time.Millisecond)
	}
	rows := []storedAdministratorAuditEvent{}
	err := app.DB().NewQuery(`
		SELECT * FROM (
			SELECT * FROM (
				SELECT 'account' AS [[kind]], 'a:' || [[id]] AS [[sort_key]], [[id]], [[created_at]] AS [[occurred_at]],
					[[action]], [[actor_id]], [[target_user_id]] AS [[target_id]], [[target_email]],
					[[from_status]] AS [[from_value]], [[to_status]] AS [[to_value]], '' AS [[method]], '' AS [[phase]],
					'' AS [[outcome]], '' AS [[reason_code]], 0 AS [[reason_status]], '' AS [[ip]], '' AS [[user_agent]]
				FROM {{leafen_administrator_account_audit_events}} INDEXED BY [[idx_leafen_administrator_account_audit_time]]
				WHERE {:account} AND ([[created_at]] < {:at} OR ([[created_at]] = {:at} AND 'a:' || [[id]] < {:key}))
				ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT {:limit}
			)
			UNION ALL
			SELECT * FROM (
				SELECT 'settings', 's:' || [[id]], [[id]], [[created_at]], [[field]], [[actor_id]], '', '',
					[[from_value]], [[to_value]], '', '', '', '', 0, '', ''
				FROM {{leafen_server_settings_audit_events}} INDEXED BY [[idx_leafen_server_settings_audit_time]]
				WHERE {:settings} AND ([[created_at]] < {:at} OR ([[created_at]] = {:at} AND 's:' || [[id]] < {:key}))
				ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT {:limit}
			)
			UNION ALL
			SELECT * FROM (
				SELECT 'sign_in', 'l:' || [[id]], [[id]], [[occurred_at]] / 1000000, 'sign_in', COALESCE([[owner_id]], ''),
					COALESCE([[owner_id]], ''), [[target_email]], '', '', [[method]], [[phase]], [[outcome]], [[reason_code]],
					[[reason_status]], [[ip]], [[user_agent]]
				FROM {{leafen_auth_audit_events}} INDEXED BY [[leafen_auth_audit_time_idx]]
				WHERE {:signIn} AND ([[occurred_at]] < {:atNanos}
					OR ([[occurred_at]] < {:atNanosNext} AND 'l:' || [[id]] < {:key}))
				ORDER BY [[occurred_at]] DESC, [[id]] DESC LIMIT {:limit}
			)
		) ORDER BY [[occurred_at]] DESC, [[sort_key]] DESC LIMIT {:limit}
	`).Bind(dbx.Params{
		"account": wants(administratorAuditKindAccount), "settings": wants(administratorAuditKindSettings),
		"signIn": wants(administratorAuditKindSignIn), "at": at, "key": key, "limit": limit,
		"atNanos": atNanos, "atNanosNext": atNanosNext,
	}).All(&rows)
	return rows, err
}

type administratorAuditPerson struct {
	ID    string `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
}

func loadAdministratorAuditPeople(app core.App, rows []storedAdministratorAuditEvent) (map[string]administratorAuditPerson, error) {
	ids := []string{}
	for _, row := range rows {
		for _, id := range []string{row.ActorID, row.TargetID} {
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	people := map[string]administratorAuditPerson{}
	if len(ids) == 0 {
		return people, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	found := []administratorAuditPerson{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[email]] FROM {{users}} WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&found); err != nil {
		return nil, err
	}
	for _, person := range found {
		people[person.ID] = person
	}
	return people, nil
}

func decodeAdministratorAuditCursor(raw string) (int64, string, bool) {
	if raw == "" {
		return math.MaxInt64, "", true
	}
	encoded, found := strings.CutPrefix(raw, administratorAuditCursorPrefix)
	if !found || len(encoded) > 256 {
		return 0, "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return 0, "", false
	}
	millis, key, found := strings.Cut(string(decoded), "|")
	at, err := strconv.ParseInt(millis, 10, 64)
	if !found || err != nil || at < 0 || key == "" {
		return 0, "", false
	}
	return at, key, true
}

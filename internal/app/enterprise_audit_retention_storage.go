package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errAuditRetentionInvalid       = errors.New(errorCodeAuditRetentionInvalid)
	errAuditRetentionUnavailable   = errors.New(errorCodeAuditRetentionUnavailable)
	errAuditRetentionPolicyInvalid = errors.New("audit retention policy is invalid")
	errAuditRetentionConflict      = errors.New("audit retention state conflict")
)

type storedAuditRetentionPolicy struct {
	OrganizationID string `db:"organization_id"`
	RetentionDays  int    `db:"retention_days"`
	Revision       int    `db:"revision"`
	UpdatedBy      string `db:"updated_by"`
	UpdatedAt      int64  `db:"updated_at"`
}

type storedAuditRetentionSweep struct {
	ID                string `db:"id"`
	LogicalDate       string `db:"logical_date"`
	StartedAt         int64  `db:"started_at"`
	Status            string `db:"status"`
	DiscoveryCursor   string `db:"discovery_cursor"`
	DiscoveryComplete int    `db:"discovery_complete"`
	LeaseToken        string `db:"lease_token"`
	LeaseUntil        int64  `db:"lease_until"`
}

type storedAuditRetentionWork struct {
	SweepID       string `db:"sweep_id"`
	Organization  string `db:"organization_id"`
	Status        string `db:"status"`
	AttemptCount  int    `db:"attempt_count"`
	DeletedCount  int    `db:"deleted_count"`
	NextAttemptAt int64  `db:"next_attempt_at"`
}

type storedAuditRetentionEvent struct {
	ID      string `db:"id"`
	Created int64  `db:"created"`
}

func loadOrganizationAuditRetentionPolicy(
	app core.App,
	organizationID string,
) (auditRetentionPolicyResponse, error) {
	return loadOrganizationAuditRetentionPolicyContext(
		context.Background(), app, organizationID,
	)
}

func loadOrganizationAuditRetentionPolicyContext(
	ctx context.Context,
	app core.App,
	organizationID string,
) (auditRetentionPolicyResponse, error) {
	row := storedAuditRetentionPolicy{}
	err := app.DB().NewQuery(`
		SELECT [[organization_id]], [[retention_days]], [[revision]], [[updated_by]], [[updated_at]]
		FROM {{leafen_organization_audit_retention_policies}}
		WHERE [[organization_id]] = {:organizationId}
	`).WithContext(ctx).Bind(dbx.Params{"organizationId": organizationID}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return auditRetentionPolicyResponse{
			OrganizationID: organizationID,
			RetentionDays:  auditRetentionDefaultDays,
		}, nil
	}
	if err != nil {
		return auditRetentionPolicyResponse{}, errors.Join(errAuditRetentionUnavailable, err)
	}
	if row.OrganizationID != organizationID || row.RetentionDays < auditRetentionMinimumDays || row.Revision < 1 ||
		row.UpdatedBy == "" || row.UpdatedAt < 0 {
		return auditRetentionPolicyResponse{}, errors.Join(
			errAuditRetentionUnavailable, errAuditRetentionPolicyInvalid,
		)
	}
	return auditRetentionPolicyResponse{
		OrganizationID: row.OrganizationID,
		RetentionDays:  row.RetentionDays,
		Revision:       row.Revision,
		UpdatedAt:      row.UpdatedAt,
	}, nil
}

// storeOrganizationAuditRetentionPolicy keeps an organization's period, or
// with 0 drops it: an organization without one keeps its audit log (the
// audit decision's 2026-10-21 22:25 addition — Leafen doesn't choose a
// period for anyone).
func storeOrganizationAuditRetentionPolicy(
	app core.App,
	organizationID string,
	retentionDays int,
	revision int,
	updatedBy string,
	updatedAt int64,
) error {
	if retentionDays == auditRetentionDefaultDays {
		_, err := app.DB().Delete(organizationAuditRetentionPoliciesTable, dbx.HashExp{"organization_id": organizationID}).Execute()
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_audit_retention_policies}}
			([[organization_id]], [[retention_days]], [[revision]], [[updated_by]], [[updated_at]])
		VALUES ({:organizationId}, {:retentionDays}, {:revision}, {:updatedBy}, {:updatedAt})
		ON CONFLICT ([[organization_id]]) DO UPDATE SET
			[[retention_days]] = excluded.[[retention_days]],
			[[revision]] = excluded.[[revision]],
			[[updated_by]] = excluded.[[updated_by]],
			[[updated_at]] = excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"organizationId": organizationID, "retentionDays": retentionDays,
		"revision": revision, "updatedBy": updatedBy, "updatedAt": updatedAt,
	}).Execute()
	return err
}

// auditRetentionCutoff is the time before which an organization's audit
// events go: 0, nothing, when it keeps them (no period) or the period
// reaches back before the start of time.
func auditRetentionCutoff(start int64, retentionDays int) int64 {
	const day = int64(24 * 60 * 60 * 1000)
	if retentionDays < auditRetentionMinimumDays || int64(retentionDays) > start/day {
		return 0
	}
	return start - int64(retentionDays)*day
}

func loadOrCreateAuditRetentionSweep(
	ctx context.Context,
	app core.App,
	logicalDate string,
	startedAt int64,
) (storedAuditRetentionSweep, error) {
	sweep := storedAuditRetentionSweep{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[logical_date]], [[started_at]], [[status]], [[discovery_cursor]],
			[[discovery_complete]], [[lease_token]], [[lease_until]]
		FROM {{leafen_enterprise_retention_sweeps}}
		WHERE [[status]] = 'active'
		ORDER BY [[logical_date]], [[id]]
		LIMIT 1
	`).WithContext(ctx).One(&sweep)
	if err == nil {
		return sweep, validateStoredAuditRetentionSweep(sweep, sweep.LogicalDate)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storedAuditRetentionSweep{}, err
	}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[logical_date]], [[started_at]], [[status]], [[discovery_cursor]],
			[[discovery_complete]], [[lease_token]], [[lease_until]]
		FROM {{leafen_enterprise_retention_sweeps}}
		WHERE [[logical_date]] = {:logicalDate}
	`).WithContext(ctx).Bind(dbx.Params{"logicalDate": logicalDate}).One(&sweep)
	if err == nil {
		return sweep, validateStoredAuditRetentionSweep(sweep, logicalDate)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return storedAuditRetentionSweep{}, err
	}
	id := organizationAuditID("enterprise-audit-retention", "sweep", logicalDate)
	_, err = app.DB().Insert(enterpriseRetentionSweepsTable, dbx.Params{
		"id": id, "logical_date": logicalDate, "started_at": startedAt,
		"status": "active", "discovery_cursor": "", "discovery_complete": 0,
		"lease_token": "", "lease_until": 0, "created_at": startedAt,
		"updated_at": startedAt, "completed_at": 0,
	}).WithContext(ctx).Execute()
	if err != nil {
		return storedAuditRetentionSweep{}, err
	}
	return storedAuditRetentionSweep{
		ID: id, LogicalDate: logicalDate, StartedAt: startedAt, Status: "active",
	}, nil
}

func validateStoredAuditRetentionSweep(
	sweep storedAuditRetentionSweep,
	logicalDate string,
) error {
	validStatus := sweep.Status == "active" || sweep.Status == "completed"
	if len(sweep.ID) != 32 || sweep.LogicalDate != logicalDate || sweep.StartedAt < 0 ||
		!validStatus || (sweep.DiscoveryComplete != 0 && sweep.DiscoveryComplete != 1) ||
		sweep.LeaseUntil < 0 {
		return errAuditRetentionUnavailable
	}
	return nil
}

func acquireAuditRetentionLease(
	ctx context.Context,
	app core.App,
	sweepID string,
	token string,
	now int64,
) (bool, error) {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_enterprise_retention_sweeps}}
		SET [[lease_token]] = {:token}, [[lease_until]] = {:leaseUntil}, [[updated_at]] = {:now}
		WHERE [[id]] = {:sweepId} AND [[status]] = 'active'
			AND ([[lease_token]] = '' OR [[lease_until]] <= {:now})
	`).WithContext(ctx).Bind(dbx.Params{
		"token": token, "leaseUntil": now + auditRetentionLeaseSeconds*1000,
		"now": now, "sweepId": sweepID,
	}).Execute()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func releaseAuditRetentionLease(
	ctx context.Context,
	app core.App,
	sweepID string,
	token string,
	now int64,
) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_enterprise_retention_sweeps}}
		SET [[lease_token]] = '', [[lease_until]] = 0, [[updated_at]] = {:now}
		WHERE [[id]] = {:sweepId} AND [[lease_token]] = {:token}
	`).WithContext(ctx).Bind(dbx.Params{"now": now, "sweepId": sweepID, "token": token}).Execute()
	return err
}

func nextAuditRetentionWakeAt(
	ctx context.Context,
	app core.App,
	sweepID string,
	now int64,
) (int64, bool, error) {
	sweep := struct {
		DiscoveryComplete int    `db:"discovery_complete"`
		LeaseToken        string `db:"lease_token"`
		LeaseUntil        int64  `db:"lease_until"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[discovery_complete]], [[lease_token]], [[lease_until]]
		FROM {{leafen_enterprise_retention_sweeps}}
		WHERE [[id]] = {:sweepId} AND [[status]] = 'active'
	`).WithContext(ctx).Bind(dbx.Params{"sweepId": sweepID}).One(&sweep)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if sweep.LeaseToken != "" && sweep.LeaseUntil > now {
		return sweep.LeaseUntil, true, nil
	}
	if sweep.DiscoveryComplete == 0 {
		return now, true, nil
	}

	var next sql.NullInt64
	err = app.DB().NewQuery(`
		SELECT MIN([[next_attempt_at]])
		FROM {{leafen_enterprise_retention_work}}
		WHERE [[sweep_id]] = {:sweepId}
			AND [[status]] IN ('ready', 'backlog', 'retry')
	`).WithContext(ctx).Bind(dbx.Params{"sweepId": sweepID}).Row(&next)
	if err != nil {
		return 0, false, err
	}
	if next.Valid {
		return next.Int64, true, nil
	}
	return now, true, nil
}

func discoverAuditRetentionOrganizations(
	ctx context.Context,
	app core.App,
	sweep storedAuditRetentionSweep,
	now int64,
) (int, bool, error) {
	type organizationRow struct {
		ID string `db:"id"`
	}
	rows := []organizationRow{}
	// Only organizations that chose a period: the others keep everything.
	if err := app.DB().NewQuery(`
		SELECT [[organization]].[[id]] FROM {{leafen_organizations}} AS [[organization]]
		JOIN {{leafen_organization_audit_retention_policies}} AS [[policy]] ON [[policy]].[[organization_id]] = [[organization]].[[id]]
		WHERE [[organization]].[[id]] > {:cursor}
		ORDER BY [[organization]].[[id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"cursor": sweep.DiscoveryCursor,
		"limit":  auditRetentionOrganizationsPerDiscovery + 1,
	}).All(&rows); err != nil {
		return 0, false, err
	}
	discoveryComplete := len(rows) <= auditRetentionOrganizationsPerDiscovery
	if !discoveryComplete {
		rows = rows[:auditRetentionOrganizationsPerDiscovery]
	}
	cursor := sweep.DiscoveryCursor
	params := dbx.Params{"sweepId": sweep.ID, "now": now}
	values := make([]string, 0, len(rows))
	for index, row := range rows {
		if row.ID == "" {
			return 0, false, errAuditRetentionUnavailable
		}
		cursor = row.ID
		key := fmt.Sprintf("organization%d", index)
		params[key] = row.ID
		values = append(values, "({:sweepId}, {:"+key+"}, 'ready', 0, 0, 0, '', {:now}, {:now}, 0)")
	}
	if len(values) > 0 {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_enterprise_retention_work}}
				([[sweep_id]], [[organization_id]], [[status]], [[attempt_count]], [[deleted_count]],
				 [[next_attempt_at]], [[last_error_code]], [[created_at]], [[updated_at]], [[completed_at]])
			VALUES ` + strings.Join(values, ",") + `
			ON CONFLICT ([[sweep_id]], [[organization_id]]) DO NOTHING
		`).WithContext(ctx).Bind(params).Execute(); err != nil {
			return 0, false, err
		}
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_enterprise_retention_sweeps}}
		SET [[discovery_cursor]] = {:cursor}, [[discovery_complete]] = {:complete}, [[updated_at]] = {:now}
		WHERE [[id]] = {:sweepId} AND [[status]] = 'active'
	`).WithContext(ctx).Bind(dbx.Params{
		"cursor": cursor, "complete": boolInt(discoveryComplete), "now": now, "sweepId": sweep.ID,
	}).Execute()
	return len(rows), discoveryComplete, err
}

func loadDueAuditRetentionWork(
	ctx context.Context,
	app core.App,
	sweepID string,
	now int64,
) ([]storedAuditRetentionWork, error) {
	rows := []storedAuditRetentionWork{}
	err := app.DB().NewQuery(`
		SELECT [[sweep_id]], [[organization_id]], [[status]], [[attempt_count]],
			[[deleted_count]], [[next_attempt_at]]
		FROM {{leafen_enterprise_retention_work}}
		WHERE [[sweep_id]] = {:sweepId} AND [[status]] IN ('ready', 'backlog', 'retry')
			AND [[next_attempt_at]] <= {:now}
		ORDER BY CASE [[status]] WHEN 'ready' THEN 0 WHEN 'retry' THEN 1 ELSE 2 END,
			[[next_attempt_at]], [[organization_id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"sweepId": sweepID, "now": now, "limit": auditRetentionOrganizationsPerSlice,
	}).All(&rows)
	return rows, err
}

// loadExpiredAuditRetentionEvents is the next of an organization's events
// past its period after the given one, leaving those a legal hold covers:
// what a person did or read, what is about a page, a workspace's.
func loadExpiredAuditRetentionEvents(
	ctx context.Context,
	app core.App,
	organizationID string,
	cutoff int64,
	after storedAuditRetentionEvent,
	limit int,
) ([]storedAuditRetentionEvent, error) {
	rows := []storedAuditRetentionEvent{}
	held := legalHoldKeepsSQL(legalHoldRecordSQL{
		Organization: "[[expired]].[[organization_id]]", Workspace: "[[expired]].[[workspace_id]]",
		Pages:  []string{"[[expired]].[[subject_id]]", "json_extract([[expired]].[[metadata_json]], '$.pageId')"},
		People: []string{"[[expired]].[[actor_id]]"},
	})
	err := app.DB().NewQuery(`
		SELECT [[expired]].[[id]], [[expired]].[[created]] FROM {{leafen_organization_audit_events}} [[expired]]
		WHERE [[expired]].[[organization_id]] = {:organizationId} AND [[expired]].[[created]] < {:cutoff}
			AND ([[expired]].[[created]], [[expired]].[[id]]) > ({:afterCreated}, {:afterId})
			AND NOT ` + held + `
		ORDER BY [[expired]].[[created]], [[expired]].[[id]]
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID, "cutoff": cutoff, "limit": limit,
		"afterCreated": after.Created, "afterId": after.ID,
	}).All(&rows)
	return rows, err
}

func deleteAuditRetentionEvents(
	ctx context.Context,
	app core.App,
	organizationID string,
	events []storedAuditRetentionEvent,
) error {
	if len(events) == 0 {
		return nil
	}
	params := dbx.Params{"organizationId": organizationID}
	placeholders := make([]string, 0, len(events))
	for index, event := range events {
		key := fmt.Sprintf("event%d", index)
		params[key] = event.ID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_organization_audit_events}}
		WHERE [[organization_id]] = {:organizationId} AND [[id]] IN (` + strings.Join(placeholders, ",") + `)
	`).WithContext(ctx).Bind(params).Execute()
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted != int64(len(events)) {
		return errAuditRetentionUnavailable
	}
	return nil
}

func updateAuditRetentionWork(
	ctx context.Context,
	app core.App,
	work storedAuditRetentionWork,
	status string,
	deleted int,
	nextAttemptAt int64,
	errorCode string,
	now int64,
) error {
	completedAt := int64(0)
	if status == "done" || status == "failed" {
		completedAt = now
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_enterprise_retention_work}}
		SET [[status]] = {:status}, [[attempt_count]] = [[attempt_count]] + 1,
			[[deleted_count]] = [[deleted_count]] + {:deleted}, [[next_attempt_at]] = {:nextAttemptAt},
			[[last_error_code]] = {:errorCode}, [[updated_at]] = {:now}, [[completed_at]] = {:completedAt}
		WHERE [[sweep_id]] = {:sweepId} AND [[organization_id]] = {:organizationId}
			AND [[status]] = {:priorStatus} AND [[attempt_count]] = {:priorAttempt}
	`).WithContext(ctx).Bind(dbx.Params{
		"status": status, "deleted": deleted, "nextAttemptAt": nextAttemptAt,
		"errorCode": errorCode, "now": now, "completedAt": completedAt,
		"sweepId": work.SweepID, "organizationId": work.Organization,
		"priorStatus": work.Status, "priorAttempt": work.AttemptCount,
	}).Execute()
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errAuditRetentionConflict
	}
	return nil
}

func completeAuditRetentionSweepIfDrained(
	ctx context.Context,
	app core.App,
	sweepID string,
	now int64,
) (bool, error) {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_enterprise_retention_sweeps}}
		SET [[status]] = 'completed', [[completed_at]] = {:now}, [[updated_at]] = {:now},
			[[lease_token]] = '', [[lease_until]] = 0
		WHERE [[id]] = {:sweepId} AND [[status]] = 'active' AND [[discovery_complete]] = 1
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_enterprise_retention_work}}
				WHERE [[sweep_id]] = {:sweepId} AND [[status]] IN ('ready', 'backlog', 'retry')
			)
	`).WithContext(ctx).Bind(dbx.Params{"now": now, "sweepId": sweepID}).Execute()
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

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

const (
	organizationStorageReservationActive    = "active"
	organizationStorageReservationReleased  = "released"
	organizationStorageReconciliationRows   = "R:"
	organizationStorageReconciliationFiles  = "F:"
	organizationStorageReconciliationUpload = "U:"
	productOrganizationStorageLockNamespace = "organization-storage-quota"
)

var (
	errOrganizationStorageLimitExceeded         = errors.New(errorCodeOrganizationStorageLimitExceeded)
	errOrganizationStorageReconciliationPending = errors.New(errorCodeOrganizationStorageReconciliationPending)
	errOrganizationStorageStateInvalid          = errors.New(errorCodeOrganizationStorageStateInvalid)
)

type storedProductOrganizationStorageUsage struct {
	OrganizationID string `db:"organization_id"`
	ReservedBytes  int64  `db:"reserved_bytes"`
	Version        int64  `db:"version"`
}

type storedProductOrganizationStorageReservation struct {
	ID             string `db:"reservation_id"`
	OrganizationID string `db:"organization_id"`
	WorkspaceID    string `db:"workspace_id"`
	Bytes          int64  `db:"bytes"`
	Status         string `db:"status"`
}

type storedProductOrganizationStorageReconciliation struct {
	OrganizationID   string `db:"organization_id"`
	AfterWorkspaceID string `db:"after_workspace_id"`
	AfterUploadID    string `db:"after_upload_id"`
	ReservedBytes    int64  `db:"reserved_bytes"`
	Revision         int64  `db:"revision"`
}

type productOrganizationStorageReconciliationResult struct {
	Processed int
	HasMore   bool
	Revision  int64
}

type productOrganizationStorageRecoveryItem struct {
	ReservationID        string         `db:"reservation_id"`
	WorkspaceID          string         `db:"workspace_id"`
	Bytes                int64          `db:"bytes"`
	ExistingID           sql.NullString `db:"existing_id"`
	ExistingOrganization sql.NullString `db:"existing_organization_id"`
	ExistingWorkspace    sql.NullString `db:"existing_workspace_id"`
	ExistingBytes        sql.NullInt64  `db:"existing_bytes"`
	ExistingStatus       sql.NullString `db:"existing_status"`
}

type productOrganizationStorageActiveReservation struct {
	ID                    string `db:"reservation_id"`
	WorkspaceID           string `db:"workspace_id"`
	WorkspaceOrganization string `db:"workspace_organization_id"`
	Bytes                 int64  `db:"bytes"`
}

func ensureOrganizationStorageUsageForNewOrganization(
	app core.App,
	organizationID string,
	now int64,
) error {
	if !app.HasTable("leafen_organization_storage_usage") {
		return nil
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_storage_usage}}
			([[organization_id]], [[reserved_bytes]], [[version]], [[updated_at]])
		VALUES ({:organizationId}, 0, 0, {:now})
		ON CONFLICT ([[organization_id]]) DO NOTHING
	`).Bind(dbx.Params{"organizationId": organizationID, "now": now}).Execute()
	return err
}

func productOrganizationStorageWorkspace(
	ctx context.Context,
	app core.App,
	workspaceID string,
) (string, error) {
	var organizationID string
	err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}}
		WHERE [[id]]={:workspaceId}`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": workspaceID,
	}).Row(&organizationID)
	if err != nil || organizationID == "" {
		return "", errors.Join(permissionDenied(), err)
	}
	return organizationID, nil
}

func prepareProductOrganizationStorageAdmission(
	ctx context.Context,
	app core.App,
	workspaceID string,
	now int64,
) (func(), error) {
	organizationID, err := productOrganizationStorageWorkspace(ctx, app, workspaceID)
	if err != nil {
		return nil, err
	}
	release := acquireProductFileOperationLock(
		productOrganizationStorageLockNamespace, organizationID,
	)
	if err := ensureProductOrganizationStorageReadyLocked(ctx, app, organizationID, now); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func ensureProductOrganizationStorageReadyLocked(
	ctx context.Context,
	app core.App,
	organizationID string,
	now int64,
) error {
	if _, err := loadProductOrganizationStorageUsage(ctx, app, organizationID); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	result, err := advanceProductOrganizationStorageReconciliationLocked(
		ctx, app, organizationID, now,
	)
	if err != nil {
		return err
	}
	if result.HasMore {
		return fmt.Errorf("%w: revision=%d", errOrganizationStorageReconciliationPending, result.Revision)
	}
	return nil
}

func reserveProductOrganizationStorage(
	ctx context.Context,
	app core.App,
	workspaceID string,
	reservationID string,
	requestedBytes int64,
	now int64,
) (storedProductOrganizationStorageReservation, error) {
	if app.IsTransactional() {
		return reserveProductOrganizationStorageInTransaction(
			ctx, app, workspaceID, reservationID, requestedBytes, now,
		)
	}
	release, err := prepareProductOrganizationStorageAdmission(ctx, app, workspaceID, now)
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	defer release()
	result := storedProductOrganizationStorageReservation{}
	err = app.RunInTransaction(func(txApp core.App) error {
		var reserveErr error
		result, reserveErr = reserveProductOrganizationStorageInTransaction(
			ctx, txApp, workspaceID, reservationID, requestedBytes, now,
		)
		return reserveErr
	})
	return result, err
}

func reserveProductOrganizationStorageInTransaction(
	ctx context.Context,
	app core.App,
	workspaceID string,
	reservationID string,
	requestedBytes int64,
	now int64,
) (storedProductOrganizationStorageReservation, error) {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" || requestedBytes <= 0 || now <= 0 {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageStateInvalid
	}
	organizationID, err := productOrganizationStorageWorkspace(ctx, app, workspaceID)
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	existing, err := loadProductOrganizationStorageReservation(ctx, app, reservationID)
	// A reservation released in this workspace is taken again under its id:
	// a move brings files in under the ids they had, and a move into this
	// workspace that failed released them, so the next try reserves them
	// again (2026-11-07: every retry of a move with files failed here).
	takenAgain := err == nil && existing.Status == organizationStorageReservationReleased &&
		existing.OrganizationID == organizationID && existing.WorkspaceID == workspaceID
	if err == nil && !takenAgain {
		if existing.OrganizationID != organizationID || existing.WorkspaceID != workspaceID ||
			existing.Bytes != requestedBytes || existing.Status != organizationStorageReservationActive {
			return storedProductOrganizationStorageReservation{}, errOrganizationStorageStateInvalid
		}
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return storedProductOrganizationStorageReservation{}, err
	}
	usage, err := loadProductOrganizationStorageUsage(ctx, app, organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageReconciliationPending
	}
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	if usage.Version == int64(^uint64(0)>>1) ||
		usage.ReservedBytes > int64(^uint64(0)>>1)-requestedBytes {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageStateInvalid
	}
	var storedLimit string
	if err := app.DB().NewQuery(`SELECT [[storage_limit_bytes]]
		FROM {{leafen_organizations}} WHERE [[id]]={:organizationId}`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID,
	}).Row(&storedLimit); err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	parsedLimit, err := decodeOrganizationStorageLimit(storedLimit)
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	limit := int64(0)
	if parsedLimit != nil {
		limit = *parsedLimit
	}
	if limit > 0 && (requestedBytes > limit || usage.ReservedBytes > limit-requestedBytes) {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageLimitExceeded
	}
	result, err := app.DB().NewQuery(`UPDATE {{leafen_organization_storage_usage}}
		SET [[reserved_bytes]]=[[reserved_bytes]]+{:bytes}, [[version]]=[[version]]+1,
			[[updated_at]]={:now}
		WHERE [[organization_id]]={:organizationId} AND [[version]]={:version}
			AND [[reserved_bytes]]={:reservedBytes}`).WithContext(ctx).Bind(dbx.Params{
		"bytes": requestedBytes, "now": now, "organizationId": organizationID,
		"version": usage.Version, "reservedBytes": usage.ReservedBytes,
	}).Execute()
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return storedProductOrganizationStorageReservation{}, errors.Join(
			errOrganizationStorageStateInvalid, err,
		)
	}
	reservation := storedProductOrganizationStorageReservation{
		ID: reservationID, OrganizationID: organizationID,
		WorkspaceID: workspaceID, Bytes: requestedBytes,
		Status: organizationStorageReservationActive,
	}
	if takenAgain {
		updated, err := app.DB().NewQuery(`UPDATE {{leafen_organization_storage_reservations}}
			SET [[status]]={:active}, [[bytes]]={:bytes}, [[updated_at]]={:now}, [[released_at]]=0
			WHERE [[reservation_id]]={:reservationId} AND [[status]]={:released}`).WithContext(ctx).Bind(dbx.Params{
			"active": organizationStorageReservationActive, "bytes": reservation.Bytes, "now": now,
			"reservationId": reservation.ID, "released": organizationStorageReservationReleased,
		}).Execute()
		if err != nil {
			return storedProductOrganizationStorageReservation{}, err
		}
		if count, err := updated.RowsAffected(); err != nil || count != 1 {
			return storedProductOrganizationStorageReservation{}, errors.Join(errOrganizationStorageStateInvalid, err)
		}
		return reservation, nil
	}
	_, err = app.DB().Insert("leafen_organization_storage_reservations", dbx.Params{
		"reservation_id": reservation.ID, "organization_id": reservation.OrganizationID,
		"workspace_id": reservation.WorkspaceID, "bytes": reservation.Bytes,
		"status": reservation.Status, "created_at": now, "updated_at": now,
		"released_at": 0,
	}).Execute()
	return reservation, err
}

func growProductOrganizationStorageReservation(
	ctx context.Context,
	app core.App,
	workspaceID string,
	reservationID string,
	targetBytes int64,
	now int64,
) (storedProductOrganizationStorageReservation, error) {
	if !app.IsTransactional() {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageStateInvalid
	}
	if strings.TrimSpace(reservationID) == "" || targetBytes <= 0 || now <= 0 {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageStateInvalid
	}
	existing, err := loadProductOrganizationStorageReservation(ctx, app, reservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return reserveProductOrganizationStorageInTransaction(
			ctx, app, workspaceID, reservationID, targetBytes, now,
		)
	}
	if err != nil || existing.WorkspaceID != workspaceID ||
		existing.Status != organizationStorageReservationActive ||
		existing.Bytes <= 0 || targetBytes < existing.Bytes {
		return storedProductOrganizationStorageReservation{}, errors.Join(
			errOrganizationStorageStateInvalid, err,
		)
	}
	if targetBytes == existing.Bytes {
		return existing, nil
	}
	organizationID, err := productOrganizationStorageWorkspace(ctx, app, workspaceID)
	if err != nil || existing.OrganizationID != organizationID {
		return storedProductOrganizationStorageReservation{}, errors.Join(
			errOrganizationStorageStateInvalid, err,
		)
	}
	usage, err := loadProductOrganizationStorageUsage(ctx, app, organizationID)
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	delta := targetBytes - existing.Bytes
	if usage.Version == int64(^uint64(0)>>1) ||
		usage.ReservedBytes > int64(^uint64(0)>>1)-delta {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageStateInvalid
	}
	var storedLimit string
	if err := app.DB().NewQuery(`SELECT [[storage_limit_bytes]]
		FROM {{leafen_organizations}} WHERE [[id]]={:organizationId}`).WithContext(ctx).Bind(dbx.Params{
		"organizationId": organizationID,
	}).Row(&storedLimit); err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	parsedLimit, err := decodeOrganizationStorageLimit(storedLimit)
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	if parsedLimit != nil && *parsedLimit > 0 &&
		(delta > *parsedLimit || usage.ReservedBytes > *parsedLimit-delta) {
		return storedProductOrganizationStorageReservation{}, errOrganizationStorageLimitExceeded
	}
	updated, err := app.DB().NewQuery(`UPDATE {{leafen_organization_storage_usage}}
		SET [[reserved_bytes]]=[[reserved_bytes]]+{:delta}, [[version]]=[[version]]+1,
			[[updated_at]]={:now}
		WHERE [[organization_id]]={:organizationId} AND [[version]]={:version}
			AND [[reserved_bytes]]={:reservedBytes}`).WithContext(ctx).Bind(dbx.Params{
		"delta": delta, "now": now, "organizationId": organizationID,
		"version": usage.Version, "reservedBytes": usage.ReservedBytes,
	}).Execute()
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	count, err := updated.RowsAffected()
	if err != nil || count != 1 {
		return storedProductOrganizationStorageReservation{}, errors.Join(
			errOrganizationStorageStateInvalid, err,
		)
	}
	updated, err = app.DB().NewQuery(`UPDATE {{leafen_organization_storage_reservations}}
		SET [[bytes]]={:bytes}, [[updated_at]]={:now}
		WHERE [[reservation_id]]={:reservationId} AND [[workspace_id]]={:workspaceId}
			AND [[bytes]]={:previousBytes} AND [[status]]={:active}`).WithContext(ctx).Bind(dbx.Params{
		"bytes": targetBytes, "now": now, "reservationId": reservationID,
		"workspaceId": workspaceID, "previousBytes": existing.Bytes,
		"active": organizationStorageReservationActive,
	}).Execute()
	if err != nil {
		return storedProductOrganizationStorageReservation{}, err
	}
	count, err = updated.RowsAffected()
	if err != nil || count != 1 {
		return storedProductOrganizationStorageReservation{}, errors.Join(
			errOrganizationStorageStateInvalid, err,
		)
	}
	existing.Bytes = targetBytes
	return existing, nil
}

func releaseProductOrganizationStorage(
	ctx context.Context,
	app core.App,
	reservationID string,
	now int64,
) error {
	if app.IsTransactional() {
		return releaseProductOrganizationStorageInTransaction(ctx, app, reservationID, now)
	}
	reservation, err := loadProductOrganizationStorageReservation(ctx, app, reservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return errOrganizationStorageStateInvalid
	}
	if err != nil {
		return err
	}
	release := acquireProductFileOperationLock(
		productOrganizationStorageLockNamespace, reservation.OrganizationID,
	)
	defer release()
	return app.RunInTransaction(func(txApp core.App) error {
		return releaseProductOrganizationStorageInTransaction(ctx, txApp, reservationID, now)
	})
}

func releaseProductOrganizationStorageIfPresent(
	ctx context.Context,
	app core.App,
	reservationID string,
	now int64,
) error {
	_, err := loadProductOrganizationStorageReservation(ctx, app, reservationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return releaseProductOrganizationStorage(ctx, app, reservationID, now)
}

func releaseProductOrganizationStorageInTransaction(
	ctx context.Context,
	app core.App,
	reservationID string,
	now int64,
) error {
	if strings.TrimSpace(reservationID) == "" || now <= 0 {
		return errOrganizationStorageStateInvalid
	}
	reservation, err := loadProductOrganizationStorageReservation(ctx, app, reservationID)
	if err != nil {
		return errors.Join(errOrganizationStorageStateInvalid, err)
	}
	if reservation.Status == organizationStorageReservationReleased {
		return nil
	}
	if reservation.Status != organizationStorageReservationActive || reservation.Bytes <= 0 {
		return errOrganizationStorageStateInvalid
	}
	usage, err := loadProductOrganizationStorageUsage(ctx, app, reservation.OrganizationID)
	if err != nil || usage.ReservedBytes < reservation.Bytes ||
		usage.Version == int64(^uint64(0)>>1) {
		return errors.Join(errOrganizationStorageStateInvalid, err)
	}
	updated, err := app.DB().NewQuery(`UPDATE {{leafen_organization_storage_usage}}
		SET [[reserved_bytes]]=[[reserved_bytes]]-{:bytes}, [[version]]=[[version]]+1,
			[[updated_at]]={:now}
		WHERE [[organization_id]]={:organizationId} AND [[version]]={:version}
			AND [[reserved_bytes]]={:reservedBytes}`).WithContext(ctx).Bind(dbx.Params{
		"bytes": reservation.Bytes, "now": now,
		"organizationId": reservation.OrganizationID,
		"version":        usage.Version, "reservedBytes": usage.ReservedBytes,
	}).Execute()
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil || count != 1 {
		return errors.Join(errOrganizationStorageStateInvalid, err)
	}
	updated, err = app.DB().NewQuery(`UPDATE {{leafen_organization_storage_reservations}}
		SET [[status]]={:released}, [[released_at]]={:now}, [[updated_at]]={:now}
		WHERE [[reservation_id]]={:reservationId} AND [[status]]={:active}`).WithContext(ctx).Bind(dbx.Params{
		"released": organizationStorageReservationReleased, "now": now,
		"reservationId": reservation.ID, "active": organizationStorageReservationActive,
	}).Execute()
	if err != nil {
		return err
	}
	count, err = updated.RowsAffected()
	if err != nil || count != 1 {
		return errors.Join(errOrganizationStorageStateInvalid, err)
	}
	return nil
}

func loadProductOrganizationStorageUsage(
	ctx context.Context,
	app core.App,
	organizationID string,
) (storedProductOrganizationStorageUsage, error) {
	usage := storedProductOrganizationStorageUsage{}
	err := app.DB().NewQuery(`SELECT [[organization_id]],[[reserved_bytes]],[[version]]
		FROM {{leafen_organization_storage_usage}} WHERE [[organization_id]]={:organizationId}`,
	).WithContext(ctx).Bind(dbx.Params{"organizationId": organizationID}).One(&usage)
	if err == nil && (usage.OrganizationID != organizationID || usage.ReservedBytes < 0 || usage.Version < 0) {
		return storedProductOrganizationStorageUsage{}, errOrganizationStorageStateInvalid
	}
	return usage, err
}

func loadProductOrganizationStorageReservation(
	ctx context.Context,
	app core.App,
	reservationID string,
) (storedProductOrganizationStorageReservation, error) {
	reservation := storedProductOrganizationStorageReservation{}
	err := app.DB().NewQuery(`SELECT [[reservation_id]],[[organization_id]],[[workspace_id]],[[bytes]],[[status]]
		FROM {{leafen_organization_storage_reservations}} WHERE [[reservation_id]]={:reservationId}`,
	).WithContext(ctx).Bind(dbx.Params{"reservationId": reservationID}).One(&reservation)
	return reservation, err
}

func organizationStorageAPIError(
	event *core.RequestEvent,
	err error,
	operation string,
	stage string,
) (error, bool) {
	status, code, message := 0, "", ""
	switch {
	case errors.Is(err, errOrganizationStorageLimitExceeded):
		status, code, message = 413, errorCodeOrganizationStorageLimitExceeded,
			"The organization storage limit was exceeded."
	case errors.Is(err, errOrganizationStorageReconciliationPending):
		status, code, message = 503, errorCodeOrganizationStorageReconciliationPending,
			"Organization storage usage is being recovered. Try again shortly."
	case errors.Is(err, errOrganizationStorageStateInvalid):
		status, code, message = 500, errorCodeOrganizationStorageStateInvalid,
			"Couldn't check the organization storage state."
	default:
		return nil, false
	}
	return writeAPIError(
		event, status, code, message, operation, stage, code, err,
		diagnosticSourcePosition(1),
	), true
}

func stableOrganizationStorageFailureCode(err error) string {
	switch {
	case errors.Is(err, errOrganizationStorageLimitExceeded):
		return errorCodeOrganizationStorageLimitExceeded
	case errors.Is(err, errOrganizationStorageReconciliationPending):
		return errorCodeOrganizationStorageReconciliationPending
	case errors.Is(err, errOrganizationStorageStateInvalid):
		return errorCodeOrganizationStorageStateInvalid
	default:
		return ""
	}
}

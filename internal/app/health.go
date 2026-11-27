package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	errorCodeHealthNotReady          = "LEAFEN_HEALTH_NOT_READY"
	healthDatabaseProbeFailureCode   = "LEAFEN_HEALTH_DATABASE_PROBE_FAILED"
	healthDatabaseProbeOperation     = "health.readiness"
	healthDatabaseProbeStage         = "database.probe"
	healthDatabaseProbeIdentityBytes = 24
)

var errHealthDatabaseProbeReceipt = errors.New("health database probe receipt invalid")
var errHealthDatabaseProbeAuthority = errors.New("health database probe authority unavailable")

func registerHealthRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/health", handleHealthReadiness)
}

func handleHealthReadiness(event *core.RequestEvent) error {
	if err := runHealthDatabaseProbe(event.Request.Context(), event.App); err != nil {
		return apiErrorWithCause(
			event,
			http.StatusServiceUnavailable,
			errorCodeHealthNotReady,
			"The Leafen server isn't ready yet.",
			healthDatabaseProbeOperation,
			healthDatabaseProbeStage,
			healthDatabaseProbeFailureCode,
			err,
		)
	}
	return event.JSON(http.StatusOK, map[string]any{
		"status":        "ok",
		"generation":    2,
		"database":      "data.db",
		"collaboration": "ygo",
		// Which history the data is in, so a device can tell a restored
		// server from the one it knew (data_epoch.go).
		"dataEpoch": currentDataEpoch(event.App),
	})
}

func runHealthDatabaseProbe(ctx context.Context, app core.App) error {
	database, ok := app.NonconcurrentDB().(*dbx.DB)
	if !ok {
		return errHealthDatabaseProbeAuthority
	}
	probeID := "health_" + security.RandomString(healthDatabaseProbeIdentityBytes)
	return database.TransactionalContext(ctx, nil, func(tx *dbx.Tx) error {
		var existing int
		if err := tx.NewQuery(`
			SELECT COUNT(*) FROM {{leafen_health_write_probes}} WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{"id": probeID}).Row(&existing); err != nil {
			return err
		}
		if existing != 0 {
			return errHealthDatabaseProbeReceipt
		}

		inserted, err := tx.Insert(healthWriteProbesTable, dbx.Params{
			"id": probeID, "created_at": time.Now().UTC().UnixMilli(),
		}).WithContext(ctx).Execute()
		if err != nil {
			return err
		}
		insertedCount, err := inserted.RowsAffected()
		if err != nil || insertedCount != 1 {
			return errHealthDatabaseProbeReceipt
		}

		deleted, err := tx.NewQuery(`
			DELETE FROM {{leafen_health_write_probes}} WHERE [[id]] = {:id}
		`).WithContext(ctx).Bind(dbx.Params{"id": probeID}).Execute()
		if err != nil {
			return err
		}
		deletedCount, err := deleted.RowsAffected()
		if err != nil || deletedCount != 1 {
			return errHealthDatabaseProbeReceipt
		}
		return nil
	})
}

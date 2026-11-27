//go:build !leafen_noapps

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The administrator's catalogs (docs/app-platform.md 16): browse every
// trusted catalog's apps, trust another catalog by its address and list key
// or by a closed network's bundle file, read one again, stop trusting one,
// and fetch an app's package for the usual review before installing it.

func registerAppCatalogRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+appPlatformCatalogsListPath, appCatalogRoute(func(*core.RequestEvent, string) error { return nil }))
	event.Router.POST(apiRoot+appPlatformCatalogAddPath, appCatalogRoute(func(event *core.RequestEvent, actorID string) error {
		input := appCatalogInput{}
		if err := json.NewDecoder(io.LimitReader(event.Request.Body, 16<<10)).Decode(&input); err != nil {
			return bodyRefusal(errAppInputInvalid, "catalog", err)
		}
		return addAppCatalog(event.App, input, actorID, time.Now())
	})).Bind(apis.BodyLimit(16 << 10))
	event.Router.POST(apiRoot+appPlatformCatalogUploadPath, appCatalogRoute(func(event *core.RequestEvent, actorID string) error {
		if err := event.Request.ParseMultipartForm(32 << 20); err != nil {
			return refuse(errAppCatalogInvalid, "the body isn't a multipart form")
		}
		file, _, err := event.Request.FormFile("bundle")
		if err != nil {
			return refuse(errAppCatalogInvalid, "bundle is missing: send the catalog's bundle file as bundle")
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, appCatalogBundleMaxBytes+1))
		if err != nil {
			return refuse(errAppCatalogInvalid, "bundle can't be read")
		}
		if len(raw) > appCatalogBundleMaxBytes {
			return refuse(errAppCatalogInvalid, "bundle is at most %d bytes", appCatalogBundleMaxBytes)
		}
		return importAppCatalogBundle(event.App, event.Request.FormValue("name"), event.Request.FormValue("publicKey"), actorID, raw, time.Now())
	})).Bind(apis.BodyLimit(appCatalogBundleMaxBytes + 1<<20))
	event.Router.POST(apiRoot+appPlatformCatalogRefreshPath, appCatalogRoute(func(event *core.RequestEvent, _ string) error {
		catalog, err := loadAppCatalog(event.App, event.Request.PathValue("catalogId"))
		if errors.Is(err, sql.ErrNoRows) {
			return errAppCatalogMissing
		}
		if err != nil {
			return err
		}
		if err := refreshAppCatalog(event.App, catalog, time.Now()); err != nil {
			return err
		}
		return applyAppCatalogRevocations(event.App, time.Now())
	}))
	event.Router.DELETE(apiRoot+appPlatformCatalogRemovePath, appCatalogRoute(func(event *core.RequestEvent, _ string) error {
		return removeAppCatalog(event.App, event.Request.PathValue("catalogId"))
	}))
	event.Router.POST(apiRoot+appPlatformCatalogAppFetchPath, func(event *core.RequestEvent) error {
		if !authorizeAdministratorRequest(event) {
			return nil
		}
		user, _ := authenticatedUser(event)
		hash, err := fetchAppCatalogPackage(event.App, event.Request.PathValue("catalogId"), event.Request.PathValue("appId"), user.Id)
		if err != nil {
			return appCatalogFailure(event, err)
		}
		return respondAppPackageReview(event, hash)
	})
}

// appCatalogRoute runs an administrator's change and answers with the
// catalogs as they now are.
func appCatalogRoute(change func(*core.RequestEvent, string) error) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		if !authorizeAdministratorRequest(event) {
			return nil
		}
		user, _ := authenticatedUser(event)
		if err := change(event, user.Id); err != nil {
			return appCatalogFailure(event, err)
		}
		browse, err := browseAppCatalogs(event.App)
		if err != nil {
			return appPlatformFailure(event, err)
		}
		return event.JSON(http.StatusOK, browse)
	}
}

func appCatalogFailure(event *core.RequestEvent, err error) error {
	message := func(prefix string) string {
		text := err.Error()
		if index := strings.Index(text, ": "); index >= 0 {
			return prefix + " " + text[index+2:]
		}
		return prefix
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return apiError(event, http.StatusNotFound, errorCodeAppPlatformInputInvalid, "No such catalog or app.")
	case errors.Is(err, errAppRevoked):
		return apiError(event, http.StatusConflict, errorCodeAppRevoked, "That version was revoked by its catalog.")
	case errors.Is(err, errAppCatalogAddressRefused):
		// Said as what the address must be, not as one that didn't answer.
		return apiError(event, http.StatusBadRequest, errorCodeAppCatalogAddressRefused,
			"Catalogs and their packages are read only from an https address, or from http on this machine.")
	case errors.Is(err, errAppCatalogUnreachable):
		return apiError(event, http.StatusBadGateway, errorCodeAppCatalogUnreachable, message("The catalog couldn't be read:"))
	case errors.Is(err, errAppCatalogInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeAppCatalogInvalid, refusalMessage("The catalog or its signature isn't valid.", refusalReason(err)))
	case errors.Is(err, errAppInputInvalid):
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid,
			refusalMessage("Name the catalog, its address and its list key.", refusalReason(err)))
	}
	return appPlatformFailure(event, err)
}

// A catalog the server doesn't trust, and an app a catalog doesn't list,
// each said as itself (2026-11-26: "No such catalog or app." named the
// catalog as the target for an app its catalog didn't list).
var (
	errAppCatalogMissing = refusedFor(http.StatusNotFound, errorCodeAppCatalogNotFound, "Check catalogId: no catalog this server "+
		"trusts has that ID. GET /admin/apps/catalogs lists them.")
	errAppCatalogAppMissing = refusedFor(http.StatusNotFound, errorCodeAppInstallNotFound, "Check appId: that catalog lists no app "+
		"with that ID. GET /admin/apps/catalogs lists each catalog's apps.")
)

// removeAppCatalog stops trusting a catalog: its packages stay installed;
// only its index and bundled files go. A catalog the server doesn't trust is
// refused as not found, and Leafen's own — the official catalog, which the
// server puts back, and the built-in apps — as staying, never answered as
// removed (2026-11-26: an unknown ID answered 200 with the catalogs, and an
// agent said it had removed it; a mistyped ID left the catalog meant trusted).
func removeAppCatalog(app core.App, id string) error {
	if id == appBuiltinCatalogID || id == appOfficialCatalogID && appOfficialCatalogURL != "" {
		return refusedFor(http.StatusConflict, errorCodeAppPlatformInputInvalid, "Check catalogId: Leafen's official catalog and its "+
			"built-in apps stay on every server, the official one put back when it is read again. To keep an app off, leave it "+
			"uninstalled, or uninstall it with DELETE /admin/apps/installs/{installId}.", apiErrorField{Name: "catalogId", Problem: "Leafen's own"})
	}
	result, err := app.DB().Delete(appCatalogsTable, dbx.HashExp{"id": id}).Execute()
	if err != nil {
		return err
	}
	if removed, err := result.RowsAffected(); err != nil || removed == 0 {
		if err == nil {
			err = errAppCatalogMissing
		}
		return err
	}
	return nil
}

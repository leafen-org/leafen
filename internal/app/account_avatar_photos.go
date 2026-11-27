package app

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// A person's uploaded photo is kept with their account, not in a workspace,
// as small as the app makes it (256 pixels square). Its address is public and
// unguessable, as Notion's are, so it shows wherever the person does; a new
// upload makes a new address.

const (
	accountPhotoMaxBytes   = 256 * 1024
	accountPhotoPathPrefix = apiRoot + "/avatars/photos/"
)

var accountPhotoExtensions = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
}

func registerAccountPhotoRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/account/avatar-photo", uploadAccountPhoto)
	event.Router.GET(apiRoot+"/avatars/photos/{file}", serveAccountPhoto)
}

// uploadAccountPhoto keeps a photo and answers its address, which the person
// then saves as their picture. Photos they neither show nor just uploaded
// are let go.
func uploadAccountPhoto(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, accountPhotoMaxBytes+1))
	if err != nil || len(body) == 0 || len(body) > accountPhotoMaxBytes {
		return apiError(event, http.StatusBadRequest, errorCodeAccountProfileInvalid, "The photo must be an image of up to 256 KB.")
	}
	contentType := http.DetectContentType(body)
	extension, known := accountPhotoExtensions[contentType]
	if !known {
		return apiError(event, http.StatusBadRequest, errorCodeAccountProfileInvalid, "The photo must be a JPEG, PNG, WebP or GIF image.")
	}
	id := strings.ToLower(security.RandomStringWithAlphabet(24, "abcdefghijklmnopqrstuvwxyz0123456789"))
	address := accountPhotoPathPrefix + id + extension
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().Insert("leafen_account_photos", dbx.Params{
			"id": id, "user_id": user.Id, "content_type": contentType, "bytes": body, "created": time.Now().UTC().UnixMilli(),
		}).Execute(); err != nil {
			return err
		}
		shown := ""
		if current, err := txApp.FindRecordById("users", user.Id); err == nil {
			shown = accountPhotoID(current.GetString("profile_avatar"))
		}
		_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_account_photos}}
			WHERE [[user_id]] = {:user} AND [[id]] != {:id} AND [[id]] != {:shown}`).
			Bind(dbx.Params{"user": user.Id, "id": id, "shown": shown}).Execute()
		return err
	})
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountProfileSaveFailed,
			"Couldn't keep the photo.", "account-photo.upload", "transaction",
			errorCodeAccountProfileSaveFailed, err,
		)
	}
	return event.JSON(http.StatusOK, map[string]string{"avatar": address})
}

// accountPhotoID is the photo an avatar value names, if it names one.
func accountPhotoID(avatar string) string {
	file, ok := strings.CutPrefix(avatar, accountPhotoPathPrefix)
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(file, ".")
	return id
}

func serveAccountPhoto(event *core.RequestEvent) error {
	id, _, _ := strings.Cut(event.Request.PathValue("file"), ".")
	photo := struct {
		ContentType string `db:"content_type"`
		Bytes       []byte `db:"bytes"`
	}{}
	err := event.App.DB().NewQuery(`SELECT [[content_type]], [[bytes]] FROM {{leafen_account_photos}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": id}).One(&photo)
	if errors.Is(err, sql.ErrNoRows) {
		return event.NoContent(http.StatusNotFound)
	}
	if err != nil {
		return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeAccountProfileReadFailed, err))
	}
	header := event.Response.Header()
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("Content-Security-Policy", "default-src 'none'")
	header.Set("X-Content-Type-Options", "nosniff")
	return event.Blob(http.StatusOK, photo.ContentType, photo.Bytes)
}

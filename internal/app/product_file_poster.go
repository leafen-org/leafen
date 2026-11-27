package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/jpeg"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// An uploaded video shows a still from about its first second before it
// plays, as Notion's does (seen 2026-11-07: its server makes
// "〈name〉_thumbnail.jpg", 640 × 360, and gives it as the video's poster), so
// a video that opens on a black frame isn't a black box on the page. Leafen's
// server reads no video, so the browser that has the video makes the still
// and sends it here: the uploader's at once, from the file it sent, or the
// first editor's that shows a video without one. The still is kept beside
// the video in its preview folder (product_file_preview.go), so it is the
// video's own: given out under the video's permission (?poster=1 on its
// address), copied with it when a page is copied, deleted with it, and left
// out of backups as every preview is, to be made again when next shown.

const (
	productFilePosterPath     = "/files/{id}/poster"
	productFilePosterName     = "poster.jpg"
	productFilePosterMaxBytes = 512 * 1024
	// The still is at most this many pixels on a side (Notion's is 640 wide).
	productFilePosterMaxSide           = 1920
	errorCodeFilePosterInvalid         = "LEAFEN_FILE_POSTER_INVALID"
	errorCodeFilePosterNotFound        = "LEAFEN_FILE_POSTER_NOT_FOUND"
	productFilePosterRequestMaxBytes   = productFilePosterMaxBytes*4/3 + 1024
	productFilePosterContentType       = "image/jpeg"
	productFilePosterQueryParameter    = "poster"
	productFilePosterQueryParameterSet = "1"
)

type productFilePosterInput struct {
	JPEG string `json:"jpeg"`
}

func registerProductFilePosterRoutes(event *core.ServeEvent) {
	event.Router.PUT(apiRoot+productFilePosterPath, saveProductFilePoster).
		Bind(apis.BodyLimit(productFilePosterRequestMaxBytes))
}

// productFileIsVideo is a stored file a video block plays.
func productFileIsVideo(record *core.Record) bool {
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(record.GetString("content_type"), ";", 2)[0]))
	return strings.HasPrefix(contentType, "video/") || contentType == "application/mp4"
}

func productFilePosterKey(record *core.Record) string {
	return record.BaseFilesPath() + "/" + productFilePreviewFolderPrefix + record.GetString("blob") + "/" + productFilePosterName
}

// saveProductFilePoster keeps the still an editor of the video's workspace
// sends; the first one kept stays, so two browsers making one at once agree.
func saveProductFilePoster(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := productFilePosterInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeFilePosterInvalid, "The video's still couldn't be read.")
	}
	still, err := base64.StdEncoding.DecodeString(input.JPEG)
	if err != nil || len(still) == 0 || len(still) > productFilePosterMaxBytes {
		return apiError(event, http.StatusBadRequest, errorCodeFilePosterInvalid, "The video's still couldn't be read.")
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(still))
	if err != nil || config.Width < 1 || config.Height < 1 ||
		config.Width > productFilePosterMaxSide || config.Height > productFilePosterMaxSide {
		return apiError(event, http.StatusBadRequest, errorCodeFilePosterInvalid, "The video's still couldn't be read.")
	}
	record, _, err := authorizeWorkspaceFile(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("id")), permissionWorkspaceWrite,
	)
	if err != nil {
		return writeProductFileReadError(event, err, false)
	}
	if !productFileIsVideo(record) {
		return apiError(event, http.StatusBadRequest, errorCodeFilePosterInvalid, "Only a video has a still.")
	}
	fsys, err := event.App.NewFilesystem()
	if err != nil {
		return err
	}
	defer fsys.Close()
	key := productFilePosterKey(record)
	if exists, err := fsys.Exists(key); err == nil && exists {
		return event.NoContent(http.StatusNoContent)
	}
	if err := fsys.Upload(still, key); err != nil {
		return err
	}
	return event.NoContent(http.StatusNoContent)
}

// productFilePosterAsked is a file read for its video's still.
func productFilePosterAsked(query url.Values) bool {
	return query.Get(productFilePosterQueryParameter) == productFilePosterQueryParameterSet
}

// serveProductFilePoster answers a read of a video's still, or says it has
// none yet, which an editor's browser then makes.
func serveProductFilePoster(event *core.RequestEvent, fsys *filesystem.System, record *core.Record) error {
	key := productFilePosterKey(record)
	if exists, err := fsys.Exists(key); err != nil || !exists || !productFileIsVideo(record) {
		event.Response.Header().Del("Content-Type")
		event.Response.Header().Del("Content-Disposition")
		return apiError(event, http.StatusNotFound, errorCodeFilePosterNotFound, "The video has no still yet.")
	}
	event.Response.Header().Set("Content-Type", productFilePosterContentType)
	return serveNamedFile(fsys, event.Response, event.Request, key, productFilePosterName)
}

// copyProductFilePoster gives a copied video its source's still, when it has
// one; a copy without one has it made when next shown.
func copyProductFilePoster(fsys *filesystem.System, source, target *core.Record) error {
	if !productFileIsVideo(source) {
		return nil
	}
	reader, err := fsys.GetReader(productFilePosterKey(source))
	if err != nil {
		return nil
	}
	defer reader.Close()
	var still bytes.Buffer
	if _, err := still.ReadFrom(reader); err != nil {
		return errors.Join(errors.New(errorCodeFilePosterInvalid), err)
	}
	return fsys.Upload(still.Bytes(), productFilePosterKey(target))
}

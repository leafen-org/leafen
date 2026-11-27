package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Every write that gives an icon or a cover an uploaded file — a page's
// (PATCH /pages/{id}/metadata), a database page's (its presentation, and its
// page's metadata), a database's (a content change of its data source), a
// template's icon and a person's profile cover — checks the file by one
// rule, the body's (content_graph_given_body_files.go): one uploaded whole
// into the workspace (availableGraphFileRecords) and, as an image block's
// and a callout icon's file, a picture (notionManagedFileContentCompatible
// with graphBlockTypeImage). Before (2026-11-26) these writes checked only
// that the file was whole, and a page's cover kept a text file. Only what a
// write changes is checked: a look stored before stays as it is, read back
// as it was written (the screen's cover, the file as its background picture,
// draws none for it), through the page's other changes until it is changed. Copies and
// imports write looks already stored and don't come here; a workspace's, a
// teamspace's and a group's icon keep their own stricter rule, raster
// pictures only (checkHeldIconFile).

// givenLookFile is one icon or cover a write gives: the request's field, the
// value given and the one stored, and whether the value may name a file (a
// cover always; an icon when its kind is a picture).
type givenLookFile struct {
	Field, Value, Stored string
	File                 bool
}

// givenCover is a cover a write gives, nil when it gives none.
func givenCover(field string, value *string, stored string) givenLookFile {
	if value == nil {
		return givenLookFile{}
	}
	return givenLookFile{Field: field, Value: *value, Stored: stored, File: true}
}

// givenImageIcon is an icon a write gives of the kind it gives, nil when it
// gives none.
func givenImageIcon(field string, value, kind *string, stored string) givenLookFile {
	if value == nil || kind == nil {
		return givenLookFile{}
	}
	return givenLookFile{Field: field, Value: *value, Stored: stored, File: *kind == graphDataSourceIconImage}
}

// checkGivenLookFiles refuses a changed icon or cover showing an uploaded
// file that isn't a picture uploaded whole into the workspace, naming the
// field, the file and what to give instead.
func checkGivenLookFiles(app core.App, workspaceID string, looks ...givenLookFile) error {
	for _, look := range looks {
		// The file stored, in either of its forms, is no change.
		fileID, managed := productFilePresentationID(look.Value)
		if storedID, _ := productFilePresentationID(look.Stored); !look.File || !managed || storedID == fileID {
			continue
		}
		field := apiErrorField{Name: look.Field, Problem: "not a picture uploaded into this workspace"}
		files, err := availableGraphFileRecords(app, workspaceID, []string{fileID})
		if err != nil {
			return err
		}
		file, whole := files[fileID]
		if !whole {
			return refusedFor(http.StatusBadRequest, errorCodeGraphReferenceInvalid, "Check "+look.Field+": file "+fileID+
				" isn't one uploaded whole into this workspace. Finish its upload, or upload the picture into this workspace and give the new one.", field)
		}
		if !notionManagedFileContentCompatible(graphBlockTypeImage, file) {
			kind := strings.TrimSpace(strings.SplitN(file.ContentType, ";", 2)[0])
			if kind == "" {
				kind = "of no type"
			}
			return refusedFor(http.StatusBadRequest, errorCodeGraphReferenceInvalid, "Check "+look.Field+": an icon or a cover "+
				"shows a picture (image/…), and "+graphFileNamed(file)+" is "+kind+". Upload a picture and give it, or an image address.", field)
		}
	}
	return nil
}

// checkGraphDataSourceLookFiles is checkGivenLookFiles for the databases a
// content change writes: each data source's new icon and cover, against the
// ones it keeps.
func checkGraphDataSourceLookFiles(app core.App, workspaceID string, operations []graphOperation) error {
	for _, operation := range operations {
		source := operation.DataSource
		if operation.Kind != graphOperationKindUpsertDataSource || source == nil || (source.Icon == nil && source.Cover == nil) {
			continue
		}
		stored := struct {
			Icon  string `db:"icon"`
			Cover string `db:"cover"`
		}{}
		_ = app.DB().NewQuery(`SELECT [[icon]], [[cover]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": source.ID}).One(&stored)
		if err := checkGivenLookFiles(app, workspaceID,
			givenImageIcon("dataSource.icon", source.Icon, source.IconType, stored.Icon),
			givenCover("dataSource.cover", source.Cover, stored.Cover),
		); err != nil {
			return err
		}
	}
	return nil
}

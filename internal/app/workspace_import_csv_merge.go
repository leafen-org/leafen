package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Importing a CSV into a database adds its rows there, as Notion's CSV
// import does with "기존 데이터베이스로 가져오기" or a database's "CSV와 병합"
// (seen 2026-10-01): each column fills the property it is matched to, a new
// property of the type chosen for it, or none. A select's name it lacks
// becomes a new option, a status names one it has, a relation links the rows
// its names title. Rows are only added, after the database's own, in the
// CSV's order. The import is an append to the page that holds the database,
// and takes its rows and new properties back when it stops short.

const (
	maxWorkspaceCSVMergeColumns      = 1000
	workspaceCSVMergeOlderIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// workspaceCSVMergeRowID is the ID the import gives its index-th row, the
// same each time, so the rows can be found again to take back. A row is a
// page, so its ID is a page's (derivedPageID).
func workspaceCSVMergeRowID(jobID string, index int) string {
	return derivedPageID("workspace-csv-merge", jobID, strconv.Itoa(index))
}

// workspaceCSVMergeJobRowID is the ID a job's index-th row has: a page's,
// or for an import admitted before its rows took page IDs, which its
// payload doesn't mark (PageIDs), the 15 characters it gave them then —
// its rows were staged with those, and are taken back by them.
func workspaceCSVMergeJobRowID(job storedJob, payload workspaceImportJobPayload, index int) string {
	if payload.PageIDs {
		return workspaceCSVMergeRowID(job.ID, index)
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("workspace-csv-merge\x00%s\x00%d", job.ID, index)))
	id := make([]byte, 15)
	for position := range id {
		id[position] = workspaceCSVMergeOlderIDAlphabet[int(digest[position])%len(workspaceCSVMergeOlderIDAlphabet)]
	}
	return string(id)
}

type workspaceCSVMergeProperty struct {
	ID       string `db:"id"`
	Type     string `db:"type"`
	Name     string `db:"name"`
	Position string `db:"position"`
	Config   string `db:"config"`
}

// workspaceCSVMergeWritable are the property types a CSV column fills.
var workspaceCSVMergeWritable = map[string]struct{}{
	graphValueTypeTitle: {}, graphValueTypeRichText: {}, graphValueTypeNumber: {}, graphValueTypeCheckbox: {},
	graphValueTypeDate: {}, graphValueTypeSelect: {}, graphValueTypeMultiSelect: {}, graphValueTypeStatus: {},
	graphValueTypeURL: {}, graphValueTypeEmail: {}, graphValueTypePhone: {}, graphValueTypeRelation: {},
	graphValueTypePerson: {},
}

// workspaceCSVMergeNewTypes are the types a column's new property takes.
var workspaceCSVMergeNewTypes = map[string]struct{}{
	graphValueTypeRichText: {}, graphValueTypeNumber: {}, graphValueTypeSelect: {}, graphValueTypeMultiSelect: {},
	graphValueTypeStatus: {}, graphValueTypeDate: {}, graphValueTypeCheckbox: {}, graphValueTypeURL: {},
	graphValueTypeEmail: {}, graphValueTypePhone: {}, graphValueTypePerson: {},
}

// workspaceCSVMergeDestination is the database a CSV's rows join.
type workspaceCSVMergeDestination struct {
	WorkspaceID  string
	DataSourceID string
	PageID       string
	Title        string
}

// loadWorkspaceCSVMergeDestination is the database a request names, when
// it takes added rows (database_added_rows.go) and the user may add rows to
// the page that holds it, as its own rows join it.
func loadWorkspaceCSVMergeDestination(event *core.RequestEvent, userID string) (workspaceCSVMergeDestination, error) {
	destination := workspaceCSVMergeDestination{
		WorkspaceID:  strings.TrimSpace(event.Request.PathValue("workspaceId")),
		DataSourceID: strings.TrimSpace(event.Request.PathValue("dataSourceId")),
	}
	err := event.App.DB().NewQuery(`
		SELECT [[page_id]], [[title]] FROM {{leafen_data_sources}}
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId}
			AND `+databaseSourceTakesAddedRowsSQL("{:id}")+`
	`).Bind(dbx.Params{
		"id": destination.DataSourceID, "workspaceId": destination.WorkspaceID,
	}).Row(&destination.PageID, &destination.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return destination, permissionDenied()
	}
	if err != nil {
		return destination, err
	}
	// Its rows go in while the database's page takes changes
	// (workspace_import_target_gone.go).
	authorization, err := admitWorkspaceImportTarget(event.App, userID, destination.WorkspaceID, destination.PageID)
	if err != nil {
		return destination, err
	}
	if strings.TrimSpace(destination.Title) == "" {
		destination.Title = authorization.Page.GetString("title")
	}
	return destination, nil
}

// readWorkspaceCSVMergeTarget is a database's properties a CSV column can
// fill, in their order.
func readWorkspaceCSVMergeTarget(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	destination, err := loadWorkspaceCSVMergeDestination(event, user.Id)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "csv-merge-target")
	}
	stored, err := loadWorkspaceCSVMergeProperties(event.App, destination.DataSourceID)
	if err != nil {
		return workspaceImportAPIError(event, err, "csv-merge-target")
	}
	target := workspaceCSVMergeTarget{
		DataSourceID: destination.DataSourceID, PageID: destination.PageID, Title: destination.Title,
		Properties: []workspaceCSVMergeTargetProperty{},
	}
	for _, property := range stored {
		if _, writable := workspaceCSVMergeWritable[property.Type]; writable {
			target.Properties = append(target.Properties, workspaceCSVMergeTargetProperty{
				ID: property.ID, Name: property.Name, Type: property.Type,
			})
		}
	}
	return event.JSON(http.StatusOK, target)
}

// loadWorkspaceCSVMergeProperties are a database's live properties in their order.
func loadWorkspaceCSVMergeProperties(app core.App, dataSourceID string) ([]workspaceCSVMergeProperty, error) {
	stored := []workspaceCSVMergeProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[position]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:dataSourceId} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).All(&stored)
	return stored, err
}

func (r *durableJobRunner) createWorkspaceCSVMerge(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	destination, err := loadWorkspaceCSVMergeDestination(event, user.Id)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "authorize-csv-merge")
	}
	input := workspaceCSVMergeInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceImportBodyRefused(event, "CSV merge request", err)
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	columnsProblem := ""
	if len(input.ColumnPropertyIDs) > maxWorkspaceCSVMergeColumns || len(input.NewProperties) > maxWorkspaceCSVMergeColumns {
		columnsProblem = fmt.Sprintf("columnPropertyIds and newProperties hold up to %d each", maxWorkspaceCSVMergeColumns)
	}
	if problem := firstNonempty(durableRequestIDProblem(input.RequestID), workspaceImportTextProblem("csv", input.CSV), columnsProblem); problem != "" {
		return workspaceImportRefused(event, problem)
	}
	return r.admitWorkspaceImport(event, user.Id, destination.WorkspaceID, workspaceImportAdmission{
		Input: workspaceImportInput{
			RequestID: input.RequestID, Format: workspaceImportFormatCSV, Title: destination.Title, CSV: input.CSV,
			CSVKeepFirstRow: input.KeepFirstRow,
		},
		TargetPageID: destination.PageID, Append: true,
		MergeDataSourceID: destination.DataSourceID, MergeColumns: input.ColumnPropertyIDs,
		MergeNewProperties: input.NewProperties,
	})
}

// workspaceImportAdmissionHash names what an import brings and where: a new
// page, or an existing page or database.
func workspaceImportAdmissionHash(workspaceID string, admission workspaceImportAdmission) string {
	input := admission.Input
	switch {
	case admission.MergeDataSourceID != "":
		created, _ := json.Marshal(admission.MergeNewProperties)
		version := "csv-merge-v1"
		if input.CSVKeepFirstRow {
			version = "csv-merge-v1-keep-first-row"
		}
		digest := sha256.Sum256([]byte(strings.Join(append([]string{
			version, workspaceID, admission.MergeDataSourceID, input.CSV, string(created),
		}, admission.MergeColumns...), "\x00")))
		return hex.EncodeToString(digest[:])
	case admission.Replace:
		return workspaceMarkdownReplaceRequestHash(workspaceID, admission.TargetPageID, input.Markdown)
	case admission.Append && !admission.FillsPlace:
		return workspaceMarkdownAppendRequestHash(workspaceID, admission.TargetPageID, input.Markdown)
	}
	// One that fills its place is the import asked for, so the same request
	// finds it whatever the page holds since.
	return workspaceImportRequestHash(workspaceID, input)
}

func workspaceCSVMergeNewPropertyIDs(properties []workspaceCSVMergeNewProperty) []string {
	ids := make([]string, 0, len(properties))
	for _, property := range properties {
		ids = append(ids, strings.TrimSpace(property.ID))
	}
	return ids
}

// parseWorkspaceImportAdmission gives what an import brings, owned by its
// job, and how many rows a CSV merge adds.
func parseWorkspaceImportAdmission(
	app core.App,
	ownerID, jobID string,
	admission workspaceImportAdmission,
) (workspaceImportItemSource, int, error) {
	if admission.MergeDataSourceID != "" {
		return parseWorkspaceCSVMerge(app, ownerID, jobID, admission)
	}
	if !admission.Append && !admission.Replace {
		// A batch's database takes the data source ID its documents' mentions name it by
		// (workspace_import_data_source_references.go).
		dataSourceID, err := workspaceImportReservedDataSourceID(app, admission)
		if err != nil {
			return nil, 0, err
		}
		items, err := parseWorkspaceImportInput(admission.Input, admission.ReservedRows, dataSourceID)
		return items, 0, err
	}
	prefix := workspaceMarkdownAppendPrefix(jobID)
	if admission.Replace {
		prefix = workspaceMarkdownReplacePrefix(jobID)
	}
	// A batch's document goes into the page its file named, whose title its
	// first heading may give (workspace_import_field_pages.go).
	title := ""
	if admission.Append && admission.BatchJobID != "" {
		title = admission.Input.Title
	}
	return ownWorkspaceMarkdownItems(prefix, workspaceImportTitledMarkdownSource(admission.Input.Markdown, title, admission.Input.Locale)), 0, nil
}

// ownWorkspaceMarkdownItems names a page's appended or replacing blocks after
// the job and their place, as they are staged: a block's parent and the
// block before it come before it. A sub-page's own block put in its place
// keeps its name, the page's; an inline database's data source, properties,
// view and rows keep theirs.
func ownWorkspaceMarkdownItems(prefix string, source workspaceImportItemSource) workspaceImportItemSource {
	return func(emit func(workspaceImportStagedItem) error) error {
		identities := map[string]string{}
		made := 0
		return source(func(item workspaceImportStagedItem) error {
			if (item.Title != nil || item.Head != nil || item.InlineDatabase != "") && item.ID == "" || item.Operation != nil && item.ID == "" {
				return emit(item)
			}
			if item.ID == "" {
				return workspaceImportStateErrorAt("markdown-own-identity")
			}
			owned := item.ID
			if item.Type != graphBlockTypeChildPage || !isStoredChildPageBlock(item.ID, item.Type, string(item.Content)) {
				owned = fmt.Sprintf("%s%08d", prefix, made)
				made++
			}
			identities[item.ID] = owned
			item.ID = owned
			for _, reference := range []*string{&item.ParentID, &item.AfterID} {
				if *reference == "" {
					continue
				}
				mapped := identities[*reference]
				if mapped == "" {
					return workspaceImportStateErrorAt("markdown-own-order")
				}
				*reference = mapped
			}
			return emit(item)
		})
	}
}

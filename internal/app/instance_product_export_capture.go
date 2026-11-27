package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type instanceProductExportCaptureCursor struct {
	RowID          int64  `json:"rowId"`
	ActiveRowID    int64  `json:"activeRowId,omitzero"`
	SourceRevision int64  `json:"sourceRevision,omitempty"`
	ColumnIndex    int    `json:"columnIndex,omitempty"`
	ColumnOffset   int64  `json:"columnOffset,omitzero"`
	ColumnKind     string `json:"columnKind,omitempty"`
	ColumnOpen     bool   `json:"columnOpen,omitzero"`
	ColumnDone     bool   `json:"columnDone,omitzero"`
}

type instanceProductExportCapturedBatch struct {
	Fragment      []byte
	Cursor        instanceProductExportCaptureCursor
	CompletedRows int
	Exhausted     bool
}

func readInstanceProductExportRows(
	app core.App,
	policy instanceProductExportTablePolicy,
	cursor instanceProductExportCaptureCursor,
	byteLimit, rowLimit int,
	sourceOwners ...string,
) (instanceProductExportCapturedBatch, error) {
	result := instanceProductExportCapturedBatch{
		Fragment: make([]byte, 0, byteLimit), Cursor: cursor,
	}
	if byteLimit < 128 || rowLimit < 1 || !validInstanceProductExportCaptureCursor(cursor, len(policy.Columns)) {
		return result, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
	}
	columns := append([]string(nil), policy.Columns...)
	sort.Strings(columns)
	sourceOwner := ""
	if len(sourceOwners) > 0 {
		sourceOwner = sourceOwners[0]
	}
	resumedRow := cursor.ActiveRowID != 0
	if resumedRow {
		changed, err := loadInstanceProductExportSourceRevision(app, cursor.SourceRevision)
		if err != nil {
			return result, newInstanceProductExportFailure(errorCodeInstanceProductExportSourceChanged, err)
		}
		if changed {
			return result, newInstanceProductExportFailure(errorCodeInstanceProductExportSourceChanged)
		}
	}
	for len(result.Fragment) < byteLimit && result.CompletedRows < rowLimit {
		if result.Cursor.ActiveRowID == 0 {
			rowID, exists, err := nextInstanceProductExportRowID(app, policy.Table, result.Cursor.RowID)
			if err != nil {
				return result, err
			}
			if !exists {
				result.Exhausted = true
				return result, nil
			}
			result.Cursor.ActiveRowID = rowID
			result.Cursor.SourceRevision, err = createInstanceProductExportSourceRevision(
				app, sourceOwner, policy.Table, rowID,
			)
			if err != nil {
				return result, err
			}
			if !appendInstanceProductExportToken(&result.Fragment, byteLimit, []byte{'{'}) {
				return result, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
			}
		}

		for result.Cursor.ColumnIndex < len(columns) {
			if len(result.Fragment) >= byteLimit {
				return result, nil
			}
			column := columns[result.Cursor.ColumnIndex]
			if !result.Cursor.ColumnOpen {
				complete, err := beginInstanceProductExportColumn(app, policy.Table, column, &result, byteLimit)
				if err != nil {
					return result, err
				}
				if complete {
					continue
				}
				if !result.Cursor.ColumnOpen {
					return result, nil
				}
			}
			if !result.Cursor.ColumnDone {
				advanced, err := appendInstanceProductExportColumnPart(app, policy.Table, column, &result, byteLimit)
				if err != nil {
					return result, err
				}
				if !advanced || !result.Cursor.ColumnDone {
					if len(result.Fragment) >= byteLimit || !advanced {
						return result, nil
					}
					continue
				}
			}
			if !appendInstanceProductExportToken(&result.Fragment, byteLimit, []byte{'"'}) {
				return result, nil
			}
			advanceInstanceProductExportColumn(&result.Cursor)
		}

		if !appendInstanceProductExportToken(&result.Fragment, byteLimit, []byte("}\n")) {
			return result, nil
		}
		if err := releaseInstanceProductExportSourceRevision(app, result.Cursor.SourceRevision); err != nil {
			return result, err
		}
		result.Cursor.RowID = result.Cursor.ActiveRowID
		result.Cursor.ActiveRowID = 0
		result.Cursor.SourceRevision = 0
		result.Cursor.ColumnIndex = 0
		result.CompletedRows++
		if resumedRow {
			return result, nil
		}
	}
	return result, nil
}

func validInstanceProductExportCaptureCursor(cursor instanceProductExportCaptureCursor, columnCount int) bool {
	if cursor.RowID < 0 || cursor.ActiveRowID < 0 || cursor.SourceRevision < 0 || cursor.ColumnIndex < 0 || cursor.ColumnIndex > columnCount || cursor.ColumnOffset < 0 {
		return false
	}
	if cursor.ActiveRowID == 0 {
		return cursor.SourceRevision == 0 && cursor.ColumnIndex == 0 && cursor.ColumnOffset == 0 && cursor.ColumnKind == "" && !cursor.ColumnOpen && !cursor.ColumnDone
	}
	if cursor.ActiveRowID <= cursor.RowID || cursor.ColumnDone && !cursor.ColumnOpen {
		return false
	}
	return true
}

func nextInstanceProductExportRowID(app core.App, table string, afterRowID int64) (int64, bool, error) {
	var rowID int64
	err := app.DB().NewQuery(
		`SELECT rowid FROM ` + app.DB().QuoteSimpleTableName(table) +
			` WHERE rowid > {:afterRowId} ORDER BY rowid LIMIT 1`,
	).Bind(dbx.Params{"afterRowId": afterRowID}).Row(&rowID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return rowID, true, nil
}

func beginInstanceProductExportColumn(
	app core.App,
	table, column string,
	result *instanceProductExportCapturedBatch,
	byteLimit int,
) (bool, error) {
	prefix, err := json.Marshal(column)
	if err != nil {
		return false, err
	}
	if result.Cursor.ColumnIndex > 0 {
		prefix = append([]byte{','}, prefix...)
	}
	prefix = append(prefix, ':')
	if len(result.Fragment)+len(prefix)+1 > byteLimit {
		return false, nil
	}
	kind, err := loadInstanceProductExportColumnKind(app, table, column, result.Cursor.ActiveRowID)
	if err != nil {
		return false, err
	}
	switch kind {
	case "null", "integer", "real":
		value, err := loadInstanceProductExportScalar(app, table, column, result.Cursor.ActiveRowID)
		if err != nil {
			return false, err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return false, err
		}
		if !appendInstanceProductExportToken(&result.Fragment, byteLimit, append(prefix, encoded...)) {
			return false, nil
		}
		advanceInstanceProductExportColumn(&result.Cursor)
		return true, nil
	case "text", "blob":
		prefix = append(prefix, '"')
		if !appendInstanceProductExportToken(&result.Fragment, byteLimit, prefix) {
			return false, nil
		}
		result.Cursor.ColumnKind = kind
		result.Cursor.ColumnOpen = true
		return false, nil
	default:
		return false, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
	}
}

func loadInstanceProductExportColumnKind(app core.App, table, column string, rowID int64) (string, error) {
	quotedColumn := app.DB().QuoteSimpleColumnName(column)
	query := `SELECT typeof(` + quotedColumn + `) FROM ` +
		app.DB().QuoteSimpleTableName(table) + ` WHERE rowid = {:rowId}`
	var kind string
	if err := app.DB().NewQuery(query).Bind(dbx.Params{"rowId": rowID}).Row(&kind); err != nil {
		return "", newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete, err)
	}
	return kind, nil
}

func loadInstanceProductExportScalar(app core.App, table, column string, rowID int64) (any, error) {
	query := `SELECT ` + app.DB().QuoteSimpleColumnName(column) + ` FROM ` +
		app.DB().QuoteSimpleTableName(table) + ` WHERE rowid = {:rowId}`
	var value any
	if err := app.DB().NewQuery(query).Bind(dbx.Params{"rowId": rowID}).Row(&value); err != nil {
		return nil, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete, err)
	}
	return value, nil
}

func appendInstanceProductExportColumnPart(
	app core.App,
	table, column string,
	result *instanceProductExportCapturedBatch,
	byteLimit int,
) (bool, error) {
	remaining := byteLimit - len(result.Fragment)
	switch result.Cursor.ColumnKind {
	case "text":
		return appendInstanceProductExportTextPart(app, table, column, result, remaining)
	case "blob":
		return appendInstanceProductExportBlobPart(app, table, column, result, remaining)
	default:
		return false, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
	}
}

func appendInstanceProductExportTextPart(
	app core.App,
	table, column string,
	result *instanceProductExportCapturedBatch,
	remaining int,
) (bool, error) {
	maxInputBytes := remaining / 6
	if maxInputBytes < 1 {
		return false, nil
	}
	var part []byte
	query := `SELECT substr(CAST(` + app.DB().QuoteSimpleColumnName(column) + ` AS BLOB), {:offset}, {:amount}) FROM ` +
		app.DB().QuoteSimpleTableName(table) + ` WHERE rowid = {:rowId}`
	err := app.DB().NewQuery(query).Bind(dbx.Params{
		"offset": result.Cursor.ColumnOffset + 1,
		"amount": maxInputBytes + utf8.UTFMax,
		"rowId":  result.Cursor.ActiveRowID,
	}).Row(&part)
	if err != nil {
		return false, err
	}
	complete := len(part) <= maxInputBytes
	if !complete {
		consumed := 0
		for consumed < maxInputBytes {
			_, size := utf8.DecodeRune(part[consumed:])
			if consumed+size > maxInputBytes {
				break
			}
			consumed += size
		}
		if consumed == 0 {
			return false, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
		}
		part = part[:consumed]
	}
	encoded, err := json.Marshal(string(part))
	if err != nil {
		return false, err
	}
	inner := encoded[1 : len(encoded)-1]
	if len(inner) > remaining {
		return false, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
	}
	result.Fragment = append(result.Fragment, inner...)
	result.Cursor.ColumnOffset += int64(len(part))
	result.Cursor.ColumnDone = complete
	return len(part) > 0 || complete, nil
}

func appendInstanceProductExportBlobPart(
	app core.App,
	table, column string,
	result *instanceProductExportCapturedBatch,
	remaining int,
) (bool, error) {
	maxBytes := remaining / 4 * 3
	if maxBytes < 3 {
		return false, nil
	}
	var part []byte
	query := `SELECT substr(` + app.DB().QuoteSimpleColumnName(column) + `, {:offset}, {:amount}) FROM ` +
		app.DB().QuoteSimpleTableName(table) + ` WHERE rowid = {:rowId}`
	err := app.DB().NewQuery(query).Bind(dbx.Params{
		"offset": result.Cursor.ColumnOffset + 1,
		"amount": maxBytes + 1,
		"rowId":  result.Cursor.ActiveRowID,
	}).Row(&part)
	if err != nil {
		return false, err
	}
	complete := len(part) <= maxBytes
	if !complete {
		part = part[:maxBytes]
	}
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(part)))
	base64.StdEncoding.Encode(encoded, part)
	if len(encoded) > remaining {
		return false, newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
	}
	result.Fragment = append(result.Fragment, encoded...)
	result.Cursor.ColumnOffset += int64(len(part))
	result.Cursor.ColumnDone = complete
	return len(part) > 0 || complete, nil
}

func appendInstanceProductExportToken(target *[]byte, limit int, token []byte) bool {
	if len(*target)+len(token) > limit {
		return false
	}
	*target = append(*target, token...)
	return true
}

func advanceInstanceProductExportColumn(cursor *instanceProductExportCaptureCursor) {
	cursor.ColumnIndex++
	cursor.ColumnOffset = 0
	cursor.ColumnKind = ""
	cursor.ColumnOpen = false
	cursor.ColumnDone = false
}

func storeInstanceProductExportFragment(
	app core.App,
	job storedJob,
	table string,
	batch instanceProductExportCapturedBatch,
) error {
	if len(batch.Fragment) == 0 || len(batch.Fragment) > instanceProductExportFragmentTargetBytes {
		return newInstanceProductExportFailure(errorCodeInstanceProductExportPolicyIncomplete)
	}
	envelope, err := json.Marshal(instanceProductExportChunkEnvelope{
		Table: table, Fragment: base64.StdEncoding.EncodeToString(batch.Fragment),
	})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(batch.Fragment)
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_job_chunks}}
			([[job_id]], [[chunk_index]], [[item_count]], [[cursor]], [[sha256]], [[created]], [[data_json]])
		VALUES ({:jobId}, {:chunkIndex}, {:itemCount}, {:cursor}, {:sha256}, {:created}, {:data})
		ON CONFLICT ([[job_id]], [[chunk_index]]) DO NOTHING
	`).Bind(dbx.Params{
		"jobId": job.ID, "chunkIndex": job.ChunkCount + 1,
		"itemCount": batch.CompletedRows, "cursor": table,
		"sha256": hex.EncodeToString(digest[:]), "created": time.Now().UTC().UnixMilli(),
		"data": string(envelope),
	}).Execute()
	return err
}

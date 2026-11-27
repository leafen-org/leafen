package organization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func downloadOrganizationAuditExport(r *leafen.DurableJobRunner, event *core.RequestEvent) error {
	authorization, ok := leafen.AuthorizeOrganizationAudit(event)
	if !ok {
		return nil
	}
	row, err := leafen.LoadStoredOrganizationAuditExport(
		event.App, authorization.OrganizationID, event.Request.PathValue("exportId"),
	)
	if err != nil {
		return leafen.OrganizationAuditError(event, err, "export-download-read")
	}
	if _, err := leafen.OrganizationAuditExportFromStored(row, authorization.OrganizationID, true); err != nil {
		return leafen.OrganizationAuditError(event, err, "export-download-validate")
	}
	if row.ArtifactVersion == 1 {
		digest := sha256.Sum256([]byte(row.Content))
		if hex.EncodeToString(digest[:]) != row.ContentSHA256 {
			return leafen.OrganizationAuditError(event, leafen.ErrOrganizationAuditUnavailable, "export-download-digest")
		}
	} else if err := streamOrganizationAuditExportArtifact(io.Discard, event.App, row); err != nil {
		return leafen.OrganizationAuditError(event, err, "export-download-digest")
	}
	contentType, suffix, err := organizationAuditExportDownloadMetadata(row.Format)
	if err != nil {
		return leafen.OrganizationAuditError(event, err, "export-download-format")
	}
	filename := fmt.Sprintf("organization-audit-%s.%s", row.ID, suffix)
	event.Response.Header().Set("Content-Type", contentType)
	event.Response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	event.Response.Header().Set("Content-Length", strconv.FormatInt(row.ContentBytes, 10))
	event.Response.Header().Set("ETag", fmt.Sprintf("\"sha256:%s\"", row.ContentSHA256))
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	event.Response.Header().Set("Cache-Control", "private, no-store")
	event.Response.WriteHeader(http.StatusOK)
	if row.ArtifactVersion == 1 {
		_, err = io.WriteString(event.Response, row.Content)
	} else {
		// The bounded preflight above preserves truthful HTTP status. This second
		// indexed pass keeps the artifact out of memory while writing the response.
		err = streamOrganizationAuditExportArtifact(event.Response, event.App, row)
	}
	if err != nil {
		return err
	}
	return recordOrganizationAuditExportServed(event.App, authorization.PrincipalID, row, filename)
}

// recordOrganizationAuditExportServed writes "감사 로그 내보내기됨" once the
// whole file went out to the person who asked, named by the file: each
// download is one line, and an export no screen took (a closed tab, a failed
// check) writes none.
func recordOrganizationAuditExportServed(app core.App, actorID string, row leafen.StoredOrganizationAuditExport, filename string) error {
	return leafen.RecordOrganizationAuditEvent(app, leafen.OrganizationAuditEvent{
		OrganizationID: row.OrganizationID, ActorID: actorID,
		Action: "organization_audit.export", TargetType: "audit_export", TargetID: row.ID,
		Metadata: map[string]any{
			"name": filename, "format": row.Format, "eventCount": row.EventCount, "contentSha256": row.ContentSHA256,
		},
		OccurredAt: time.Now().UTC().UnixMilli(),
	})
}

func organizationAuditExportDownloadMetadata(format string) (string, string, error) {
	switch format {
	case leafen.OrganizationAuditExportFormatJSONL:
		return "application/x-ndjson; charset=utf-8", "jsonl", nil
	case leafen.OrganizationAuditExportFormatCSV:
		return "text/csv; charset=utf-8", "csv", nil
	case leafen.OrganizationAuditExportFormatJSON:
		return "application/json; charset=utf-8", "json", nil
	default:
		return "", "", leafen.ErrOrganizationAuditUnavailable
	}
}

func streamOrganizationAuditExportArtifact(
	writer io.Writer,
	app core.App,
	row leafen.StoredOrganizationAuditExport,
) error {
	hasher := sha256.New()
	output := io.MultiWriter(writer, hasher)
	written := int64(0)
	eventCount := 0
	expectedIndex := 1
	afterIndex := 0
	for {
		chunks := []storedOrganizationAuditExportChunk{}
		err := app.DB().NewQuery(`
			SELECT [[chunk_index]], [[item_count]], [[data_json]], [[sha256]]
			FROM {{leafen_job_chunks}}
			WHERE [[job_id]] = {:jobId} AND [[chunk_index]] > {:afterIndex}
			ORDER BY [[chunk_index]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": row.ID, "afterIndex": afterIndex,
			"limit": leafen.OrganizationAuditExportDownloadChunkWindow,
		}).All(&chunks)
		if err != nil {
			return err
		}
		if len(chunks) == 0 {
			break
		}
		for _, chunk := range chunks {
			fragment := ""
			if chunk.Index != expectedIndex || chunk.ItemCount < 1 ||
				json.Unmarshal([]byte(chunk.DataJSON), &fragment) != nil {
				return leafen.ErrOrganizationAuditUnavailable
			}
			digest := sha256.Sum256([]byte(fragment))
			if chunk.SHA256 != hex.EncodeToString(digest[:]) {
				return leafen.ErrOrganizationAuditUnavailable
			}
			wireFragment := organizationAuditExportWireFragment(fragment, row.Format, expectedIndex > 1)
			count, err := io.WriteString(output, wireFragment)
			if err != nil {
				return err
			}
			written += int64(count)
			eventCount += chunk.ItemCount
			afterIndex = chunk.Index
			expectedIndex++
		}
	}
	if expectedIndex == 1 {
		empty, err := leafen.SerializeOrganizationAuditEvents(nil, row.Format)
		if err != nil {
			return err
		}
		count, err := io.WriteString(output, empty)
		if err != nil {
			return err
		}
		written += int64(count)
	} else if row.Format == leafen.OrganizationAuditExportFormatJSON {
		count, err := io.WriteString(output, "]")
		if err != nil {
			return err
		}
		written += int64(count)
	}
	if expectedIndex-1 != row.ChunkCount || eventCount != row.EventCount ||
		written != row.ContentBytes || hex.EncodeToString(hasher.Sum(nil)) != row.ContentSHA256 {
		return leafen.ErrOrganizationAuditUnavailable
	}
	return nil
}

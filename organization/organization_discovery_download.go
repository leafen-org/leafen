package organization

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

func downloadOrganizationDiscoveryExport(r *leafen.DurableJobRunner, event *core.RequestEvent) error {
	authorization, ok := authorizeOrganizationDiscovery(event)
	if !ok {
		return nil
	}
	row, err := loadStoredOrganizationDiscoveryExport(
		event.App, authorization.OrganizationID, event.Request.PathValue("exportId"),
	)
	if err != nil {
		return organizationDiscoveryError(event, err, "export-download-read")
	}
	if _, err := organizationDiscoveryExportFromStored(row, authorization.OrganizationID, false); err != nil {
		return organizationDiscoveryError(event, err, "export-download-validate")
	}
	if err := streamOrganizationDiscoveryArtifact(io.Discard, event.App, row); err != nil {
		return organizationDiscoveryError(event, err, "export-download-digest")
	}
	contentType, suffix, err := organizationDiscoveryDownloadMetadata(row.Format)
	if err != nil {
		return organizationDiscoveryError(event, err, "export-download-format")
	}
	filename := fmt.Sprintf("organization-discovery-%s.%s", row.ID, suffix)
	event.Response.Header().Set("Content-Type", contentType)
	event.Response.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	event.Response.Header().Set("Content-Length", strconv.FormatInt(row.ByteCount, 10))
	event.Response.Header().Set("ETag", fmt.Sprintf("\"sha256:%s\"", row.ContentSHA256))
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	event.Response.Header().Set("Cache-Control", "private, no-store")
	event.Response.WriteHeader(http.StatusOK)
	if err := streamOrganizationDiscoveryArtifact(event.Response, event.App, row); err != nil {
		return err
	}
	// "증거 자료 내보내기됨" once the whole file went out, named by the file.
	return leafen.RecordOrganizationAuditEvent(event.App, leafen.OrganizationAuditEvent{
		OrganizationID: row.OrganizationID, ActorID: authorization.PrincipalID,
		Action: "organization_discovery.export", TargetType: "organization_discovery_export", TargetID: row.ID,
		Metadata: map[string]any{
			"name": filename, "itemCount": row.ItemCount, "format": row.Format, "contentSha256": row.ContentSHA256,
		},
		OccurredAt: time.Now().UTC().UnixMilli(),
	})
}

func organizationDiscoveryDownloadMetadata(format string) (string, string, error) {
	switch format {
	case leafen.OrganizationDiscoveryExportFormatJSONL:
		return "application/x-ndjson; charset=utf-8", "jsonl", nil
	case leafen.OrganizationDiscoveryExportFormatJSON:
		return "application/json; charset=utf-8", "json", nil
	default:
		return "", "", errOrganizationDiscoveryUnavailable
	}
}

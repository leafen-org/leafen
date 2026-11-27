package app

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"time"
)

func serializeOrganizationAuditEvents(
	events []organizationAuditEventResponse,
	format string,
) (string, error) {
	var content []byte
	var err error
	switch format {
	case organizationAuditExportFormatJSONL:
		buffer := bytes.Buffer{}
		encoder := json.NewEncoder(&buffer)
		for _, event := range events {
			if err := encoder.Encode(event); err != nil {
				return "", failedBecause(errOrganizationAuditUnavailable, err)
			}
			if buffer.Len() > organizationAuditExportChunkMaxBytes {
				return "", errOrganizationAuditTooLarge
			}
		}
		content = buffer.Bytes()
	case organizationAuditExportFormatCSV:
		buffer := bytes.Buffer{}
		writer := csv.NewWriter(&buffer)
		if err := writer.Write([]string{
			"occurredAt", "actor", "actorId", "action", "target", "targetType", "targetId", "metadata",
		}); err != nil {
			return "", failedBecause(errOrganizationAuditUnavailable, err)
		}
		for _, event := range events {
			metadata, marshalErr := json.Marshal(event.Metadata)
			if marshalErr != nil {
				return "", failedBecause(errOrganizationAuditUnavailable, marshalErr)
			}
			targetName := ""
			if event.TargetName != nil {
				targetName = *event.TargetName
			}
			// Each cell is kept from running as a formula as Leafen's other CSV
			// files keep theirs (csvText): a name may start with "=".
			record := []string{
				time.UnixMilli(event.OccurredAt).UTC().Format(time.RFC3339), event.ActorName, event.ActorID, event.Action,
				targetName, event.TargetType, event.TargetID, string(metadata),
			}
			for index, cell := range record {
				record[index] = csvText(cell)
			}
			if err := writer.Write(record); err != nil {
				return "", failedBecause(errOrganizationAuditUnavailable, err)
			}
			writer.Flush()
			if writer.Error() != nil {
				return "", errOrganizationAuditUnavailable
			}
			if buffer.Len() > organizationAuditExportChunkMaxBytes {
				return "", errOrganizationAuditTooLarge
			}
		}
		writer.Flush()
		if writer.Error() != nil {
			return "", errOrganizationAuditUnavailable
		}
		content = buffer.Bytes()
	case organizationAuditExportFormatJSON:
		content, err = json.Marshal(events)
		if err != nil {
			return "", failedBecause(errOrganizationAuditUnavailable, err)
		}
	default:
		return "", errOrganizationAuditInvalid
	}
	if len(content) > organizationAuditExportChunkMaxBytes {
		return "", errOrganizationAuditTooLarge
	}
	return string(content), nil
}

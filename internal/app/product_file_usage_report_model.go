package app

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A running report no screen has moved on for this long is abandoned, and
// the next request for the same report makes it anew. A step moves it within
// seconds.
const fileUsageReportAbandonedAfter = 2 * time.Minute

var (
	errFileUsageReportInvalid             = errors.New(errorCodeFileUsageReportInvalid)
	errFileUsageReportContinuationInvalid = errors.New(errorCodeFileUsageReportContinuationInvalid)
	errFileUsageReportStateInvalid        = errors.New(errorCodeFileUsageReportStateInvalid)
	errFileUsageReportUnavailable         = errors.New(errorCodeFileUsageReportUnavailable)
)

type productFileUsageReportStat struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

type productFileUsageReportAccumulator struct {
	Totals        fileUsageReportTotals                 `json:"totals"`
	Pending       fileUsageReportPending                `json:"pending"`
	ByStatus      map[string]productFileUsageReportStat `json:"byStatus"`
	ByScope       map[string]productFileUsageReportStat `json:"byScope"`
	ByContentType map[string]productFileUsageReportStat `json:"byContentType"`
	Largest       []fileUsageReportFile                 `json:"largest"`
	Recent        []fileUsageReportFile                 `json:"recent"`
}

type productFileUsageReportSource struct {
	ID          string `db:"id"`
	WorkspaceID string `db:"workspace_id"`
	Name        string `db:"name"`
	ContentType string `db:"content_type"`
	Size        int64  `db:"size"`
	Status      string `db:"status"`
	CreatedAt   string `db:"created_at"`
}

func emptyProductFileUsageReportAccumulator() productFileUsageReportAccumulator {
	return productFileUsageReportAccumulator{
		ByStatus:      map[string]productFileUsageReportStat{},
		ByScope:       map[string]productFileUsageReportStat{},
		ByContentType: map[string]productFileUsageReportStat{},
		Largest:       []fileUsageReportFile{},
		Recent:        []fileUsageReportFile{},
	}
}

func normalizeProductFileUsageReportAccumulator(
	accumulator *productFileUsageReportAccumulator,
) error {
	if accumulator == nil || accumulator.Totals.Files < 0 || accumulator.Totals.Bytes < 0 ||
		accumulator.Totals.ActiveStorageBytes < 0 || accumulator.Totals.PendingBytes < 0 ||
		accumulator.Pending.Active < 0 || accumulator.Pending.Expired < 0 {
		return errFileUsageReportStateInvalid
	}
	if accumulator.ByStatus == nil {
		accumulator.ByStatus = map[string]productFileUsageReportStat{}
	}
	if accumulator.ByScope == nil {
		accumulator.ByScope = map[string]productFileUsageReportStat{}
	}
	if accumulator.ByContentType == nil {
		accumulator.ByContentType = map[string]productFileUsageReportStat{}
	}
	if accumulator.Largest == nil {
		accumulator.Largest = []fileUsageReportFile{}
	}
	if accumulator.Recent == nil {
		accumulator.Recent = []fileUsageReportFile{}
	}
	return nil
}

func addProductFileUsageReportSources(
	accumulator productFileUsageReportAccumulator,
	sources []productFileUsageReportSource,
) (productFileUsageReportAccumulator, error) {
	if err := normalizeProductFileUsageReportAccumulator(&accumulator); err != nil {
		return accumulator, err
	}
	for _, source := range sources {
		if source.ID == "" || source.WorkspaceID == "" || source.Size < 0 ||
			(source.Status != "uploaded" && source.Status != "pending" && source.Status != "expired") {
			return accumulator, errFileUsageReportStateInvalid
		}
		item := fileUsageReportFile{
			ID: source.ID, WorkspaceID: source.WorkspaceID, Name: source.Name,
			ContentType: source.ContentType, Size: source.Size,
			Status: source.Status, CreatedAt: source.CreatedAt,
		}
		accumulator.Totals.Files++
		accumulator.Totals.Bytes += source.Size
		bumpProductFileUsageReportStat(accumulator.ByStatus, source.Status, source.Size, false)
		bumpProductFileUsageReportStat(accumulator.ByScope, "uploads", source.Size, false)
		bumpProductFileUsageReportStat(
			accumulator.ByContentType, strings.TrimSpace(source.ContentType), source.Size, true,
		)
		switch source.Status {
		case "uploaded":
			accumulator.Totals.UploadedBytes += source.Size
			accumulator.Totals.ActiveStorageBytes += source.Size
		case "pending":
			accumulator.Totals.PendingBytes += source.Size
			accumulator.Pending.Active++
		case "expired":
			accumulator.Totals.ExpiredBytes += source.Size
			accumulator.Pending.Expired++
		}
		accumulator.Recent = mergeProductFileUsageReportTop(
			accumulator.Recent, item, fileUsageReportRecentUploads, false,
		)
		if source.Status == "uploaded" {
			accumulator.Largest = mergeProductFileUsageReportTop(
				accumulator.Largest, item, fileUsageReportLargestUploads, true,
			)
		}
	}
	return accumulator, nil
}

func bumpProductFileUsageReportStat(
	stats map[string]productFileUsageReportStat,
	key string,
	bytes int64,
	bounded bool,
) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = "unknown"
	}
	if bounded {
		if _, exists := stats[key]; !exists && len(stats) >= fileUsageReportMaxBuckets-1 {
			key = "other"
		}
	}
	value := stats[key]
	value.Count++
	value.Bytes += bytes
	stats[key] = value
}

func mergeProductFileUsageReportTop(
	items []fileUsageReportFile,
	incoming fileUsageReportFile,
	limit int,
	bySize bool,
) []fileUsageReportFile {
	byID := make(map[string]fileUsageReportFile, len(items)+1)
	for _, item := range items {
		byID[item.ID] = item
	}
	byID[incoming.ID] = incoming
	result := make([]fileUsageReportFile, 0, len(byID))
	for _, item := range byID {
		result = append(result, item)
	}
	sort.Slice(result, func(left, right int) bool {
		if bySize && result[left].Size != result[right].Size {
			return result[left].Size > result[right].Size
		}
		if !bySize && result[left].CreatedAt != result[right].CreatedAt {
			return result[left].CreatedAt > result[right].CreatedAt
		}
		return result[left].ID > result[right].ID
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func productFileUsageReportBuckets(
	stats map[string]productFileUsageReportStat,
) []fileUsageReportBucket {
	keys := make([]string, 0, len(stats))
	for key := range stats {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]fileUsageReportBucket, 0, len(keys))
	for _, key := range keys {
		value := stats[key]
		result = append(result, fileUsageReportBucket{
			Key: key, Count: value.Count, Bytes: value.Bytes,
		})
	}
	return result
}

func productFileUsageReportResponseFrom(
	job storedProductFileUsageReportJob,
	accumulator productFileUsageReportAccumulator,
) (fileUsageReportResponse, error) {
	maintenanceRuns, err := decodeProductFileUsageReportMaintenanceRuns(
		job.MaintenanceRunsJSON, job.MaintenanceLimit,
	)
	if err != nil {
		return fileUsageReportResponse{}, err
	}
	response := fileUsageReportResponse{
		Partial: job.Status != fileUsageReportStatusCompleted,
		FileUsageReportJob: fileUsageReportJob{
			ID: job.ID, RequestID: job.RequestID, ReportType: job.ReportType,
			WorkspaceID: job.WorkspaceID, OrganizationID: job.OrganizationID,
			Status: job.Status, Phase: job.Phase, Revision: job.Revision,
			StartedAt: job.StartedAt, CompletedAt: job.CompletedAt,
		},
		Totals: accumulator.Totals, Pending: accumulator.Pending,
		ByStatus:       productFileUsageReportBuckets(accumulator.ByStatus),
		ByScope:        productFileUsageReportBuckets(accumulator.ByScope),
		ByContentType:  productFileUsageReportBuckets(accumulator.ByContentType),
		LargestUploads: accumulator.Largest, RecentUploads: accumulator.Recent,
		MaintenanceRuns: maintenanceRuns,
		WorkspaceCount:  job.WorkspaceCount,
		ByWorkspace:     []fileUsageReportWorkspaceResult{},
	}
	if job.StorageLimitBytes != "" {
		if value, err := strconv.ParseInt(job.StorageLimitBytes, 10, 64); err == nil && value >= 0 {
			response.StorageLimitBytes = &value
		}
	}
	return response, nil
}

func decodeProductFileUsageReportMaintenanceRuns(
	value string,
	limit int,
) ([]fileUsageReportMaintenanceRun, error) {
	runs := []fileUsageReportMaintenanceRun{}
	if limit < 1 || limit > fileMaintenanceRunRetentionPerWorkspace ||
		json.Unmarshal([]byte(value), &runs) != nil || runs == nil || len(runs) > limit {
		return nil, errFileUsageReportStateInvalid
	}
	identities := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		stats := productFileMaintenanceWorkspaceStats{
			Scanned: run.Scanned, Expired: run.Expired,
			DeletedObjects: run.DeletedObjects, FailedObjects: run.FailedObjects,
			DiagnosticReference: run.DiagnosticReference,
		}
		if strings.TrimSpace(run.ID) == "" || strings.TrimSpace(run.WorkspaceID) == "" ||
			run.Kind != productFileMaintenanceRunKind || run.Scanned < 0 ||
			!validProductFileMaintenanceStats(stats) || run.DeletedObjects+run.FailedObjects > run.Scanned ||
			run.ScheduledAt <= 0 || run.StartedAt < run.ScheduledAt || run.FinishedAt < run.StartedAt ||
			run.Status != productFileMaintenanceRunStatus(stats) ||
			(run.FailedObjects > 0) != (strings.TrimSpace(run.DiagnosticReference) != "") {
			return nil, errFileUsageReportStateInvalid
		}
		if _, duplicate := identities[run.ID]; duplicate {
			return nil, errFileUsageReportStateInvalid
		}
		identities[run.ID] = struct{}{}
	}
	for index := 1; index < len(runs); index++ {
		previous, current := runs[index-1], runs[index]
		if previous.StartedAt < current.StartedAt ||
			(previous.StartedAt == current.StartedAt && previous.ID <= current.ID) {
			return nil, errFileUsageReportStateInvalid
		}
	}
	return runs, nil
}

func encodeProductFileUsageReportMaintenanceRuns(
	runs []fileUsageReportMaintenanceRun,
	limit int,
) (string, error) {
	encoded, err := json.Marshal(runs)
	if err != nil {
		return "", errors.Join(errFileUsageReportStateInvalid, err)
	}
	if _, err := decodeProductFileUsageReportMaintenanceRuns(string(encoded), limit); err != nil {
		return "", err
	}
	return string(encoded), nil
}

package app

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	notionImportRetentionDeleteBatchSize = 20
	notionImportRetentionKeepMax         = 25
	notionImportRetentionDefaultDays     = 14
	notionImportRetentionMaximumDays     = 365

	notionImportRetentionEnvironment = "LEAFEN_NOTION_IMPORT_JOB_RETENTION_DAYS"
	notionImportRetentionRunID       = "notion-import"

	notionImportRetentionStatusIdle   = "idle"
	notionImportRetentionStatusActive = "active"

	notionImportRetentionReasonAge     = "age"
	notionImportRetentionReasonKeepCap = "keep_cap"

	notionImportRetentionPhaseSources   = "sources"
	notionImportRetentionPhaseMappings  = "mappings"
	notionImportRetentionPhaseFileTasks = "file_tasks"
	notionImportRetentionPhaseChunks    = "chunks"
	notionImportRetentionPhaseReport    = "report"
	notionImportRetentionPhaseEffect    = "effect"
	notionImportRetentionPhaseJob       = "job"

	errorCodeNotionImportRetentionFailed = "LEAFEN_NOTION_IMPORT_RETENTION_FAILED"
)

var (
	errNotionImportRetentionUnavailable = errors.New(errorCodeNotionImportRetentionFailed)
	errNotionImportCancelTooLate        = errors.New("notion import has begun to show its pages")
	errNotionImportRetentionClaimed     = errors.New("notion import metadata retirement already claimed this job")
)

var notionImportRetentionPhases = [...]string{
	notionImportRetentionPhaseStaging,
	notionImportRetentionPhaseSources,
	notionImportRetentionPhaseMappings,
	notionImportRetentionPhaseFileTasks,
	notionImportRetentionPhaseChunks,
	notionImportRetentionPhaseReport,
	notionImportRetentionPhaseEffect,
	notionImportRetentionPhaseJob,
}

func notionImportRetentionDuration(raw string) time.Duration {
	days, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || days < 1 || days > notionImportRetentionMaximumDays {
		days = notionImportRetentionDefaultDays
	}
	return time.Duration(days) * 24 * time.Hour
}

func notionImportRetentionTerminal(status string) bool {
	return status == durableJobStatusCompleted ||
		status == durableJobStatusFailed ||
		status == durableJobStatusCanceled
}

func notionImportRetentionNextPhase(current string) string {
	for index, phase := range notionImportRetentionPhases {
		if phase == current && index+1 < len(notionImportRetentionPhases) {
			return notionImportRetentionPhases[index+1]
		}
	}
	return ""
}

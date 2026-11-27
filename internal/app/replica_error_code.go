package app

import "errors"

// replicationErrorCode names why a replica's place didn't answer, for the
// primary that writes it and the standby that follows it alike: another
// server's replica there, this one's or the primary's not there (or not
// opening), a primary's data from before the schema baseline, which the
// standby doesn't take over (replica_standby.go), else what the place itself
// said, as a backup's (2026-11-07: a standby whose place went said only
// "백업하지 못했습니다").
func replicationErrorCode(err error) string {
	switch {
	case errors.Is(err, errReplicaOwnedElsewhere):
		return errorCodeReplicaOwnedElsewhere
	case errors.Is(err, errReplicaMissing), errors.Is(err, errStandbyReplicaUnreadable):
		return errorCodeReplicaUnavailable
	case errors.Is(err, errSchemaHistoryBeforeBaseline):
		return errorCodeDataBeforeSchemaBaseline
	}
	return scheduledBackupErrorCode(err)
}

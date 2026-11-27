package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const importedPeopleDiscoveryPageLimit = 32

type importedPeoplePageRow struct {
	ID      string `db:"id"`
	Created string `db:"created"`
}

func admitImportedPeopleMappingJob(
	app core.App,
	principalID, workspaceID string,
	input notionImportedPeopleApplyInput,
) (storedImportedPeopleMappingJob, map[string]string, error) {
	mappings, encoded, requestID, err := normalizedImportedPeopleMappings(input)
	if err != nil {
		return storedImportedPeopleMappingJob{}, nil, err
	}
	requestHash := importedPeopleMappingRequestHash(workspaceID, requestID, encoded)
	job := storedImportedPeopleMappingJob{}
	err = app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeWorkspace(txApp, principalID, workspaceID, importedPeopleMappingPermission)
		if err != nil {
			return err
		}
		existing, err := loadImportedPeopleMappingJobByRequest(txApp, principalID, requestID)
		if err == nil {
			if existing.WorkspaceID != workspaceID || existing.RequestHash != requestHash ||
				(input.Continuation != "" && input.Continuation != existing.ID) {
				return errImportedPeopleMappingFailed
			}
			job = existing
			if job.Status == "pending" {
				return validateImportedPeopleTargets(txApp, authorization, mappings)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) || input.Continuation != "" {
			if errors.Is(err, sql.ErrNoRows) {
				return errImportedPeopleMappingFailed
			}
			return err
		}
		if err := validateImportedPeopleTargets(txApp, authorization, mappings); err != nil {
			return err
		}
		if err := releaseStalledImportedPeopleMappingJob(txApp, workspaceID); err != nil {
			return err
		}
		if err := pruneImportedPeopleMappingJobs(txApp, workspaceID); err != nil {
			return err
		}
		snapshot := importedPeoplePageRow{}
		if err := txApp.DB().NewQuery(`
			SELECT [[id]], [[created]] FROM {{leafen_pages}}
			WHERE [[workspace]]={:workspaceId} AND [[owner]]={:owner} AND [[notion_import_staging]]=FALSE
				AND [[data_source_id]]=''
			ORDER BY [[created]] DESC, [[id]] DESC LIMIT 1
		`).Bind(dbx.Params{"workspaceId": workspaceID, "owner": authorization.OwnerID}).One(&snapshot); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		job = storedImportedPeopleMappingJob{
			ID: importedPeopleMappingJobID(principalID, requestID), ActorID: principalID,
			OwnerID: authorization.OwnerID, WorkspaceID: workspaceID,
			OrganizationID: authorization.Organization.OrganizationID,
			RequestID:      requestID, RequestHash: requestHash, MappingsJSON: string(encoded),
			Status: "pending", Phase: "discovering", SnapshotCreated: snapshot.Created,
			SnapshotPageID: snapshot.ID, MappedPeople: len(mappings), CreatedAt: now, UpdatedAt: now,
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_imported_people_mapping_jobs}}
				([[id]],[[actor_id]],[[owner_id]],[[workspace_id]],[[organization_id]],[[request_id]],
				 [[request_hash]],[[mappings_json]],[[status]],[[phase]],[[snapshot_created]],[[snapshot_page_id]],
				 [[mapped_people]],[[created_at]],[[updated_at]])
			VALUES ({:id},{:actorId},{:ownerId},{:workspaceId},{:organizationId},{:requestId},
				{:requestHash},{:mappingsJSON},'pending','discovering',{:snapshotCreated},{:snapshotPageId},
				{:mappedPeople},{:now},{:now})
		`).Bind(dbx.Params{
			"id": job.ID, "actorId": job.ActorID, "ownerId": job.OwnerID,
			"workspaceId": job.WorkspaceID, "organizationId": job.OrganizationID,
			"requestId": job.RequestID, "requestHash": job.RequestHash,
			"mappingsJSON": job.MappingsJSON, "snapshotCreated": job.SnapshotCreated,
			"snapshotPageId": job.SnapshotPageID, "mappedPeople": job.MappedPeople, "now": now,
		}).Execute()
		if err == nil {
			return nil
		}
		if !isSQLiteConstraintError(err) {
			return err
		}
		replayed, replayErr := loadImportedPeopleMappingJobByRequest(txApp, principalID, requestID)
		if replayErr == nil && replayed.WorkspaceID == workspaceID && replayed.RequestHash == requestHash {
			job = replayed
			return nil
		}
		return errImportedPeopleMappingFailed
	})
	return job, mappings, err
}

func loadImportedPeopleMappingJobByRequest(app core.App, actorID, requestID string) (storedImportedPeopleMappingJob, error) {
	job := storedImportedPeopleMappingJob{}
	err := app.DB().NewQuery(importedPeopleMappingJobSelect + `
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&job)
	return job, err
}

func loadImportedPeopleMappingJobByID(app core.App, jobID string) (storedImportedPeopleMappingJob, error) {
	job := storedImportedPeopleMappingJob{}
	err := app.DB().NewQuery(importedPeopleMappingJobSelect + ` WHERE [[id]]={:id}`).Bind(dbx.Params{"id": jobID}).One(&job)
	return job, err
}

const importedPeopleMappingJobSelect = `
	SELECT [[id]],[[actor_id]],[[owner_id]],[[workspace_id]],[[organization_id]],[[request_id]],
		[[request_hash]],[[mappings_json]],[[status]],[[phase]],[[discovery_created]],[[discovery_page_id]],
		[[snapshot_created]],[[snapshot_page_id]],[[mapped_people]],[[generation]],[[changed_pages]],[[changed_blocks]],
		[[created_at]],[[updated_at]],[[completed_at]]
	FROM {{leafen_imported_people_mapping_jobs}}
`

// validateImportedPeopleTargets takes the workspace's members, and for a
// Notion migration's plan (notion_migration_people.go) its invitees and
// leaving a person out.
func validateImportedPeopleTargets(app core.App, authorization workspaceAuthorization, mappings map[string]string) error {
	targets := make([]string, 0, len(mappings))
	for _, targetID := range mappings {
		targets = append(targets, targetID)
	}
	eligible, err := eligibleImportedPeopleTargets(app, authorization.WorkspaceID, targets)
	if err != nil {
		return err
	}
	invitees, err := loadPeopleValueInvitees(app, authorization.WorkspaceID, targets)
	if err != nil {
		return err
	}
	for _, targetID := range targets {
		if _, invited := invitees[targetID]; !eligible[targetID] && !invited && targetID != importedPeopleCleared {
			return errImportedPeopleMappingFailed
		}
	}
	return nil
}

// eligibleImportedPeopleTargets reports which of the people imported people
// may be mapped to: the workspace's owner and its members, while active in
// its organization.
func eligibleImportedPeopleTargets(app core.App, workspaceID string, targets []string) (map[string]bool, error) {
	eligible := map[string]bool{}
	if len(targets) == 0 {
		return eligible, nil
	}
	unique := map[string]struct{}{}
	for _, targetID := range targets {
		unique[targetID] = struct{}{}
	}
	params := dbx.Params{"workspaceId": workspaceID}
	placeholders := make([]string, 0, len(unique))
	for targetID := range unique {
		key := fmt.Sprintf("target%d", len(placeholders))
		params[key] = targetID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []struct {
		ID string `db:"id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT DISTINCT [[user]].[[id]] AS [[id]]
		FROM {{users}} [[user]] JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]={:workspaceId}
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]]=[[workspace]].[[organization]]
			AND [[organization_member]].[[user]]=[[user]].[[id]] AND [[organization_member]].[[status]]='active'
		WHERE [[user]].[[id]] IN (` + joinSQL(placeholders) + `) AND ([[user]].[[id]]=[[workspace]].[[workspace_owner]] OR EXISTS (
			SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
			WHERE [[member]].[[workspace_id]]=[[workspace]].[[id]] AND [[member]].[[user_id]]=[[user]].[[id]]
				AND [[member]].[[role]]='member'))
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		eligible[row.ID] = true
	}
	return eligible, nil
}

func discoverImportedPeopleMappingPages(app core.App, principalID string, job *storedImportedPeopleMappingJob, mappings map[string]string) (bool, error) {
	complete := false
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := loadImportedPeopleMappingJobByID(txApp, job.ID)
		if err != nil {
			return err
		}
		if current.Status != "pending" || current.Phase != "discovering" {
			*job = current
			return nil
		}
		authorization, err := authorizeWorkspace(txApp, principalID, current.WorkspaceID, importedPeopleMappingPermission)
		if err != nil {
			return err
		}
		if authorization.OwnerID != current.OwnerID || authorization.Organization.OrganizationID != current.OrganizationID {
			return permissionDenied()
		}
		if err := validateImportedPeopleTargets(txApp, authorization, mappings); err != nil {
			return err
		}
		pages := []importedPeoplePageRow{}
		if current.SnapshotPageID != "" {
			err = txApp.DB().NewQuery(`
				SELECT [[page]].[[id]], [[page]].[[created]] FROM {{leafen_pages}} [[page]]
				WHERE [[page]].[[workspace]]={:workspaceId} AND [[page]].[[owner]]={:owner} AND [[page]].[[notion_import_staging]]=FALSE
					AND [[page]].[[data_source_id]]=''
					AND (([[page]].[[created]]>{:afterCreated}) OR ([[page]].[[created]]={:afterCreated} AND [[page]].[[id]]>{:afterId}))
					AND (([[page]].[[created]]<{:snapshotCreated}) OR ([[page]].[[created]]={:snapshotCreated} AND [[page]].[[id]]<={:snapshotId}))
					AND ` + importedPeoplePageMarkedSQL("[[page]].[[id]]") + `
				ORDER BY [[page]].[[created]], [[page]].[[id]] LIMIT {:limit}
			`).Bind(dbx.Params{
				"workspaceId": current.WorkspaceID, "owner": current.OwnerID,
				"afterCreated": current.DiscoveryCreated, "afterId": current.DiscoveryPageID,
				"snapshotCreated": current.SnapshotCreated, "snapshotId": current.SnapshotPageID,
				"limit": importedPeopleDiscoveryPageLimit + 1,
			}).All(&pages)
			if err != nil {
				return err
			}
		}
		hasMore := len(pages) > importedPeopleDiscoveryPageLimit
		if hasMore {
			pages = pages[:importedPeopleDiscoveryPageLimit]
		}
		now := time.Now().UTC().UnixMilli()
		for _, page := range pages {
			if _, err := txApp.DB().NewQuery(`
				INSERT OR IGNORE INTO {{leafen_imported_people_mapping_items}}
					([[job_id]],[[page_id]],[[page_created]],[[created_at]],[[updated_at]])
				VALUES ({:jobId},{:pageId},{:pageCreated},{:now},{:now})
			`).Bind(dbx.Params{"jobId": current.ID, "pageId": page.ID, "pageCreated": page.Created, "now": now}).Execute(); err != nil {
				return err
			}
			current.DiscoveryCreated, current.DiscoveryPageID = page.Created, page.ID
		}
		if !hasMore {
			current.Phase = "applying"
			complete = true
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_imported_people_mapping_jobs}}
			SET [[discovery_created]]={:created},[[discovery_page_id]]={:pageId},[[phase]]={:phase},
				[[generation]]=[[generation]]+1,[[updated_at]]={:now}
			WHERE [[id]]={:id} AND [[status]]='pending' AND [[phase]]='discovering'
		`).Bind(dbx.Params{"created": current.DiscoveryCreated, "pageId": current.DiscoveryPageID, "phase": current.Phase, "now": now, "id": current.ID}).Execute()
		*job = current
		return err
	})
	return complete, err
}

func nextImportedPeopleMappingItems(app core.App, jobID string) ([]storedImportedPeopleMappingItem, error) {
	items := []storedImportedPeopleMappingItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[page_id]],[[page_created]],[[applied]],[[changed_page]],[[changed_blocks]]
		FROM {{leafen_imported_people_mapping_items}}
		WHERE [[job_id]]={:jobId} AND [[applied]]=0
		ORDER BY [[page_created]],[[page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "limit": importedPeopleApplyPageLimit}).All(&items)
	return items, err
}

func markImportedPeopleMappingItem(app core.App, jobID, pageID string, changedPage, changedBlocks int) error {
	now := time.Now().UTC().UnixMilli()
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_imported_people_mapping_items}}
		WHERE [[job_id]]={:jobId} AND [[page_id]]={:pageId} AND [[applied]]=0
	`).Bind(dbx.Params{"jobId": jobID, "pageId": pageID}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return errImportedPeopleItemAlreadyApplied
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_imported_people_mapping_jobs}}
		SET [[changed_pages]]=[[changed_pages]]+{:changedPage},
			[[changed_blocks]]=[[changed_blocks]]+{:changedBlocks},
			[[generation]]=[[generation]]+1,[[updated_at]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='pending'
	`).Bind(dbx.Params{"changedPage": changedPage, "changedBlocks": changedBlocks, "now": now, "jobId": jobID}).Execute()
	return err
}

// keepImportedPeopleMappingItemSkipped records a page the mapping passed over
// with imported people left on it: a job's skipped pages are the items it
// keeps done.
func keepImportedPeopleMappingItemSkipped(app core.App, jobID, pageID string) error {
	now := time.Now().UTC().UnixMilli()
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_imported_people_mapping_items}} SET [[applied]]=1,[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[page_id]]={:pageId} AND [[applied]]=0
	`).Bind(dbx.Params{"jobId": jobID, "pageId": pageID, "now": now}).Execute()
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return errImportedPeopleItemAlreadyApplied
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_imported_people_mapping_jobs}}
		SET [[generation]]=[[generation]]+1,[[updated_at]]={:now}
		WHERE [[id]]={:jobId} AND [[status]]='pending'
	`).Bind(dbx.Params{"now": now, "jobId": jobID}).Execute()
	return err
}

func countSkippedImportedPeopleMappingPages(app core.App, jobID string) (int, error) {
	skipped := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_imported_people_mapping_items}} WHERE [[job_id]]={:jobId} AND [[applied]]=1`).
		Bind(dbx.Params{"jobId": jobID}).Row(&skipped)
	return skipped, err
}

// releaseStalledImportedPeopleMappingJob makes way for a new mapping in a
// workspace whose pending one hasn't moved for a while; one still moving is
// waited for.
func releaseStalledImportedPeopleMappingJob(app core.App, workspaceID string) error {
	pending := struct {
		ID        string `db:"id"`
		UpdatedAt int64  `db:"updated_at"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[updated_at]] FROM {{leafen_imported_people_mapping_jobs}}
		WHERE [[workspace_id]]={:workspaceId} AND [[status]]='pending'
	`).Bind(dbx.Params{"workspaceId": workspaceID}).One(&pending)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if time.Now().UTC().UnixMilli()-pending.UpdatedAt < importedPeopleStalledAfterMillis {
		return errImportedPeopleMappingBusy
	}
	return deleteImportedPeopleMappingJob(app, pending.ID)
}

// releaseImportedPeopleMappingJob lets go of a pending mapping its asker stops
// going on with, so one asked again needn't wait for it. What it mapped stays.
func releaseImportedPeopleMappingJob(app core.App, actorID, requestID string) error {
	job, err := loadImportedPeopleMappingJobByRequest(app, actorID, requestID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != "pending") {
		return nil
	}
	if err != nil {
		return err
	}
	return deleteImportedPeopleMappingJob(app, job.ID)
}

func deleteImportedPeopleMappingJob(app core.App, jobID string) error {
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_imported_people_mapping_items}} WHERE [[job_id]]={:jobId}`).
		Bind(dbx.Params{"jobId": jobID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_imported_people_mapping_jobs}} WHERE [[id]]={:jobId} AND [[status]]='pending'`).
		Bind(dbx.Params{"jobId": jobID}).Execute()
	return err
}

func pruneImportedPeopleMappingJobs(app core.App, workspaceID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_imported_people_mapping_jobs}}
		WHERE [[workspace_id]]={:workspaceId} AND [[status]]='completed'
			AND (SELECT COUNT(*) FROM {{leafen_imported_people_mapping_jobs}}
				WHERE [[workspace_id]]={:workspaceId} AND [[status]]='completed') >= {:keepMax}
			AND [[id]] IN (
				SELECT [[id]] FROM {{leafen_imported_people_mapping_jobs}}
				WHERE [[workspace_id]]={:workspaceId} AND [[status]]='completed'
				ORDER BY [[completed_at]],[[id]] LIMIT {:deleteBatch}
			)
	`).Bind(dbx.Params{
		"workspaceId": workspaceID,
		"keepMax":     importedPeopleRetentionKeepMax,
		"deleteBatch": importedPeopleRetentionBatch,
	}).Execute()
	return err
}

func finishImportedPeopleMappingJob(app core.App, principalID string, job *storedImportedPeopleMappingJob, mappings map[string]string) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := loadImportedPeopleMappingJobByID(txApp, job.ID)
		if err != nil {
			return err
		}
		authorization, err := authorizeWorkspace(txApp, principalID, current.WorkspaceID, importedPeopleMappingPermission)
		if err != nil {
			return err
		}
		if current.Status == "completed" {
			*job = current
			return nil
		}
		if current.Phase != "applying" || authorization.OwnerID != current.OwnerID {
			return errImportedPeopleMappingFailed
		}
		if err := validateImportedPeopleTargets(txApp, authorization, mappings); err != nil {
			return err
		}
		var pending int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_imported_people_mapping_items}} WHERE [[job_id]]={:jobId} AND [[applied]]=0`).Bind(dbx.Params{"jobId": current.ID}).Row(&pending); err != nil {
			return err
		}
		if pending != 0 {
			*job = current
			return nil
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`UPDATE {{leafen_imported_people_mapping_jobs}}
			SET [[status]]='completed',[[phase]]='completed',[[generation]]=[[generation]]+1,
				[[completed_at]]={:now},[[updated_at]]={:now}
			WHERE [[id]]={:id} AND [[status]]='pending'`).Bind(dbx.Params{"now": now, "id": current.ID}).Execute(); err != nil {
			return err
		}
		current.Status, current.Phase, current.CompletedAt, current.UpdatedAt = "completed", "completed", now, now
		current.Generation++
		*job = current
		return nil
	})
}

func decodeImportedPeopleJobMappings(job storedImportedPeopleMappingJob) (map[string]string, error) {
	mappings := map[string]string{}
	if err := json.Unmarshal([]byte(job.MappingsJSON), &mappings); err != nil {
		return nil, err
	}
	return mappings, nil
}

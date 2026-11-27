package app

import (
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func bootstrapProduct(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	noteAdministratorAddress(event, user.Id)
	workspaceLimit, err := bootstrapLimit(event.Request.URL.Query().Get("workspaceLimit"), maxBootstrapWorkspaces)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeBootstrapCursorInvalid, "Check the initial sync range.")
	}
	pageLimit, err := bootstrapLimit(event.Request.URL.Query().Get("pageLimit"), maxBootstrapPages)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeBootstrapCursorInvalid, "Check the initial sync range.")
	}
	// The pages of a workspace made since a moment, ahead of the whole list (bootstrap_new_pages.go).
	if since := event.Request.URL.Query().Get("createdSince"); since != "" {
		return bootstrapNewPages(event, user, event.Request.URL.Query().Get("workspaceId"), since, pageLimit)
	}
	cursor, err := decodeBootstrapCursor(event.Request.URL.Query().Get("cursor"), user.Id)
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeBootstrapCursorInvalid, "Check the initial sync position.")
	}
	ready, err := ensureBootstrapVisibilityProjectionReadiness(
		event, user.Id, errorCodeBootstrapFailed, "Couldn't load the workspace.", "bootstrap",
	)
	if !ready {
		return err
	}

	response := bootstrapResponse{}
	// The list reads one moment in a read snapshot (read_snapshot.go), not on
	// the write connection: in a transaction there it waited out every change
	// under way (2026-11-14, NAS condition: 1–4.9 s through an API write
	// burst, 0.1 s without).
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		snapshotRevision, err := readBootstrapSnapshotRevision(txApp)
		if err != nil {
			return err
		}
		// What the person may see, at which the read of the list begins
		// again when it changed between two parts (bootstrap_reader_authority.go).
		authority, err := bootstrapReaderAuthority(txApp, user.Id)
		if err != nil {
			return err
		}
		// The page list changes the list holds from here (bootstrap_page_changes.go).
		pageChangeSeq, err := currentPageListChangeSeq(txApp)
		if err != nil {
			return err
		}
		response = bootstrapResponse{
			SnapshotRevision:  snapshotRevision,
			AuthorityRevision: authority,
			Workspaces:        []workspaceResponse{}, Pages: []pageResponse{},
			PageFavorites:          []pageFavoriteProjection{},
			WorkspaceRefreshTokens: []workspaceRefreshTokenResponse{},
			DataEpoch:              currentDataEpoch(event.App),
			PageChangeSeq:          pageChangeSeq,
		}
		if !cursor.WorkspacesDone {
			// The person's deletions still to finish come with the first window.
			if cursor.WorkspaceCreated == "" && cursor.WorkspaceID == "" {
				deleting, err := loadDeletingWorkspaces(txApp, user.Id)
				if err != nil {
					return err
				}
				if len(deleting) > 0 {
					response.DeletingWorkspaces = deleting
				}
				// So are the database pages among the person's favorites.
				favorites, err := bootstrapDatabasePageFavorites(txApp, user.Id)
				if err != nil {
					return err
				}
				response.PageFavorites = append(response.PageFavorites, favorites...)
			}
			rows, done, err := loadBootstrapWorkspaces(txApp, user.Id, cursor, workspaceLimit)
			if err != nil {
				return err
			}
			workspaceIDs := make([]string, 0, len(rows))
			for _, row := range rows {
				workspaceIDs = append(workspaceIDs, row.ID)
			}
			snapshots, err := readWorkspaceChangeSnapshots(txApp, user.Id, workspaceIDs)
			if err != nil {
				return err
			}
			for _, row := range rows {
				role := ""
				if row.OwnerID != user.Id {
					role = row.Role
				}
				refreshToken, err := sealWorkspaceRefreshToken(user, snapshots[row.ID])
				if err != nil {
					return err
				}
				teamspaceRevision := int(snapshots[row.ID].PermissionCursor)
				workspace := workspaceResponse{
					ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name,
					SettingsRevision: row.SettingsRevision, Role: role, Restricted: row.Restricted, NoPrivatePages: row.NoPrivatePages, MemberCount: row.MemberCount, AppInstallID: row.AppInstallID, AppSlug: row.AppSlug,
					PageAnalyticsEnabled:                  boolPointer(!row.AnalyticsDisabled),
					PageAnalyticsAnonymousTrackingEnabled: boolPointer(!row.AnonymousDisabled),
					// Every teamspace change moves it (teamspace_mutation.go), and
					// the teamspace list is read at it: the sidebar reads the list
					// again only once it moved. Zero is a revision too.
					TeamspaceRevision: &teamspaceRevision,
				}
				if row.SettingsRevision > 0 {
					workspace.Icon = stringPointer(row.Icon)
					workspace.Domain = stringPointer(row.Domain)
					workspace.LandingPageID = row.LandingPageID
				}
				response.Workspaces = append(response.Workspaces, workspace)
				response.WorkspaceRefreshTokens = append(
					response.WorkspaceRefreshTokens,
					workspaceRefreshTokenResponse{WorkspaceID: row.ID, RefreshToken: refreshToken},
				)
				cursor.WorkspaceCreated = row.Created
				cursor.WorkspaceID = row.ID
			}
			cursor.WorkspacesDone = done
		}
		pageRows := 0
		if !cursor.PagesDone {
			rows, workspaceCreated, done, err := loadBootstrapPages(txApp, user.Id, cursor, pageLimit)
			if err != nil {
				return err
			}
			// The window moves on past pages a teamspace group has no access to.
			visible, err := withoutHiddenBootstrapPages(txApp, user.Id, rows)
			if err != nil {
				return err
			}
			if err := appendBootstrapPages(txApp, user.Id, &response, visible); err != nil {
				return err
			}
			shared, err := sharedPrivatePageIDs(txApp, visible)
			if err != nil {
				return err
			}
			response.SharedPageIDs = append(response.SharedPageIDs, shared...)
			if len(rows) > 0 {
				last := rows[len(rows)-1]
				cursor.PageWorkspaceCreated, cursor.PageWorkspace = workspaceCreated[last.WorkspaceID], last.WorkspaceID
				cursor.PageTeamspace, cursor.PageCreated, cursor.PageID = last.TeamspaceID, last.Created, last.ID
			}
			pageRows = len(rows)
			cursor.PagesDone = done
		}
		// Pages shared with the reader follow their other pages, within the
		// same window size.
		if cursor.PagesDone && !cursor.SharedDone && pageRows < pageLimit {
			// The workspaces the person is a guest in come once, with their
			// shared pages.
			if cursor.SharedCreated == "" && cursor.SharedPageID == "" {
				if err := appendBootstrapGuestWorkspaces(txApp, user, &response); err != nil {
					return err
				}
			}
			rows, done, err := loadBootstrapSharedPages(txApp, user.Id, cursor, pageLimit-pageRows)
			if err != nil {
				return err
			}
			if err := appendBootstrapPages(txApp, user.Id, &response, rows); err != nil {
				return err
			}
			// A page the person keeps privately is theirs, not shared with them
			// (pending_guest_private_pages.go).
			owned, err := privatelyOwnedPageIDs(txApp, user.Id, rows)
			if err != nil {
				return err
			}
			wide, err := workspaceWidePageIDs(txApp, user.Id, rows)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if !owned[row.ID] {
					response.SharedPageIDs = append(response.SharedPageIDs, row.ID)
					if wide[row.ID] {
						response.WorkspaceWidePageIDs = append(response.WorkspaceWidePageIDs, row.ID)
					}
				}
			}
			if len(rows) > 0 {
				cursor.SharedCreated, cursor.SharedPageID = rows[len(rows)-1].Created, rows[len(rows)-1].ID
			}
			cursor.SharedDone = done
		}
		// The inline databases this person keeps among favorites, on the pages this batch carries.
		pageIDs := make(map[string]bool, len(response.Pages))
		for _, page := range response.Pages {
			pageIDs[page.ID] = true
		}
		favorites, err := bootstrapDatabaseFavorites(txApp, user.Id, pageIDs)
		if err != nil {
			return err
		}
		response.DatabaseFavorites = favorites
		// The databases those pages hold in their bodies, which the sidebar lists under them.
		inlineDatabases, err := bootstrapInlineDatabases(txApp, user.Id, pageIDs)
		if err != nil {
			return err
		}
		response.InlineDatabases = inlineDatabases
		return nil
	})
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeBootstrapFailed,
			"Couldn't load the workspace.", "bootstrap",
			"workspace_change_snapshot", errorCodeWorkspaceChangeProbeFailed, err,
		)
	}
	if !cursor.WorkspacesDone || !cursor.PagesDone || !cursor.SharedDone {
		next, err := encodeBootstrapCursor(cursor)
		if err != nil {
			return apiErrorFrom(event, http.StatusInternalServerError, errorCodeBootstrapFailed, "Couldn't load the workspace.", err)
		}
		response.NextCursor = &next
		response.HasMore = true
	}
	return event.JSON(http.StatusOK, response)
}

// appendBootstrapGuestWorkspaces adds the workspaces the person is a guest
// in, with the refresh tokens their changes are followed by.
func appendBootstrapGuestWorkspaces(app core.App, user *core.Record, response *bootstrapResponse) error {
	rows, err := loadBootstrapGuestWorkspaces(app, user.Id)
	if err != nil {
		return err
	}
	for _, row := range rows {
		snapshot, err := readGuestWorkspaceChangeSnapshot(app, user.Id, row.ID)
		if isPermissionDenied(err) {
			continue
		}
		if err != nil {
			return err
		}
		refreshToken, err := sealWorkspaceRefreshToken(user, snapshot)
		if err != nil {
			return err
		}
		workspace := workspaceResponse{
			ID: row.ID, OrganizationID: row.OrganizationID, Name: row.Name,
			SettingsRevision: row.SettingsRevision, Guest: true,
		}
		if record, err := app.FindRecordById(workspacesCollection, row.ID); err == nil {
			if workspace.PendingPrivatePages, err = pendingGuestPrivatePages(app, user.Id, record); err != nil {
				return err
			}
		}
		if row.SettingsRevision > 0 {
			workspace.Icon = stringPointer(row.Icon)
			workspace.Domain = stringPointer(row.Domain)
		}
		response.Workspaces = append(response.Workspaces, workspace)
		response.WorkspaceRefreshTokens = append(
			response.WorkspaceRefreshTokens,
			workspaceRefreshTokenResponse{WorkspaceID: row.ID, RefreshToken: refreshToken},
		)
	}
	return nil
}

// appendBootstrapPages adds a window of pages to the response, with the
// reader's favorites of them, each titled as its reader reads it
// (page_name_mentions.go).
func appendBootstrapPages(app core.App, readerID string, response *bootstrapResponse, rows []storedBootstrapPage) error {
	pageIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		pageIDs = append(pageIDs, row.ID)
	}
	wikiMetadata, err := loadWikiPageMetadataSet(app, pageIDs)
	if err != nil {
		return err
	}
	buttonTemplates, err := loadButtonTemplatePageSet(app, pageIDs)
	if err != nil {
		return err
	}
	fieldAnchored, unmentioned, err := loadPageSidebarAnchorSets(app, pageIDs)
	if err != nil {
		return err
	}
	first := len(response.Pages)
	for _, row := range rows {
		page := pageResponse{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Title: row.Title, Content: row.Content,
			TeamspaceID: row.TeamspaceID,
			Revision:    row.Revision, Cursor: row.Cursor, ParentID: row.ParentID, Position: row.Position,
			IsLocked: row.IsLocked, LockedBy: row.LockedBy,
			InTrash: row.InTrash, TrashedAt: optionalPageTrashTimestamp(row.TrashedAt),
			ArchivedAt: optionalPageTrashTimestamp(row.ArchivedAt), ArchivedBy: row.ArchivedBy,
			Icon: row.Icon, IconType: row.IconType,
			Cover: row.Cover, CoverPosition: row.CoverPosition,
			Font: row.Font, SmallText: row.SmallText, FullWidth: row.FullWidth,
			BacklinksDisplay: row.BacklinksDisplay, PageCommentsDisplay: row.PageCommentsDisplay,
			TableOfContentsDisplay: normalizedPageTableOfContentsDisplay(&row.TableOfContentsDisplay), InlineCommentsDisplay: normalizedPageInlineCommentsDisplay(&row.InlineCommentsDisplay),
			Kind: readPageKind(row.Kind), MovedDatabase: row.MovedDatabase, ButtonTemplate: buttonTemplates[row.ID],
			FieldAnchored: fieldAnchored[row.ID], AnchorUnmentioned: unmentioned[row.ID], ViewBlock: row.ViewBlock, DatabaseContainerID: row.DatabaseContainerID,
			DatabaseBlockID: row.DatabaseBlockID,
			HasContent:      boolPointer(row.HasContent),
			CreatedBy:       row.CreatedBy, LastEditedBy: row.LastEditedBy,
			CreatedAt: row.Created, UpdatedAt: row.UpdatedAt, WikiRevision: row.WikiRevision,
			VerifiedAt: row.VerifiedAt, VerifiedBy: row.VerifiedBy, VerificationExpiresAt: row.VerificationExpiresAt,
		}
		if metadata, exists := wikiMetadata[row.ID]; exists {
			page = applyWikiMetadataToPageResponse(page, metadata)
		}
		response.Pages = append(response.Pages, page)
		response.PageFavorites = append(response.PageFavorites, pageFavoriteProjection{
			PageID: row.ID, Favorite: row.Favorite, Revision: row.FavoriteRevision,
		})
		if row.Cursor > response.Cursor {
			response.Cursor = row.Cursor
		}
	}
	titles := make(map[string]*string, len(rows))
	listed := make([]*pageResponse, 0, len(rows))
	for index := first; index < len(response.Pages); index++ {
		titles[response.Pages[index].ID] = &response.Pages[index].Title
		listed = append(listed, &response.Pages[index])
	}
	pageTitlesAsRead(app, readerID, titles, true)
	pagesTitleSpansAsRead(app, readerID, listed)
	return nameUntitledDatabasesBySources(app, listed)
}

func loadBootstrapWorkspaces(
	app core.App,
	principalID string,
	cursor bootstrapCursor,
	limit int,
) ([]storedBootstrapWorkspace, bool, error) {
	rows := []storedBootstrapWorkspace{}
	err := app.DB().NewQuery(`
		SELECT [[workspace]].[[id]], [[workspace]].[[organization]], [[workspace]].[[name]],
			[[workspace]].[[icon]], [[workspace]].[[domain]], [[settings]].[[revision]] AS [[settings_revision]],
			[[workspace]].[[workspace_owner]] AS [[owner]], [[workspace]].[[landing_page_id]], [[workspace]].[[app_install_id]],
			COALESCE((SELECT [[installed_app]].[[slug]] FROM {{leafen_app_installs}} [[installed_app]]
				WHERE [[installed_app]].[[id]] = [[workspace]].[[app_install_id]]), '') AS [[app_slug]],
			[[workspace]].[[page_analytics_disabled]], [[workspace]].[[page_analytics_anonymous_tracking_disabled]],
			CASE WHEN [[workspace]].[[workspace_owner]] = {:principalId} THEN '' ELSE [[visibility]].[[role]] END AS [[role]],
			` + workspaceRestrictedMemberSQL("[[workspace]].[[id]]", "{:principalId}") + ` AS [[restricted]],
			` + restrictedPrivatePagesOffSQL("[[workspace]].[[id]]", "[[workspace]].[[organization]]", "{:principalId}") + ` AS [[no_private_pages]],
			[[visibility]].[[workspace_created]] AS [[created]],
			` + workspaceMemberCountSQL("[[workspace]].[[id]]") + ` AS [[member_count]]
		FROM {{leafen_bootstrap_workspace_visibility}} [[visibility]]
			INDEXED BY [[idx_leafen_bootstrap_workspace_visibility_principal_order]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		CROSS JOIN {{leafen_workspace_settings_state}} [[settings]]
		CROSS JOIN {{leafen_organizations}} [[organization]]
		CROSS JOIN {{leafen_organization_memberships}} [[organization_membership]]
		WHERE [[visibility]].[[principal_id]] = {:principalId}
			AND [[workspace]].[[id]] = [[visibility]].[[workspace_id]]
			AND [[settings]].[[workspace_id]] = [[workspace]].[[id]]
			AND [[workspace]].[[created]] = [[visibility]].[[workspace_created]]
			AND [[workspace]].[[deletion_job_id]] = ''
			AND [[organization]].[[id]] = [[workspace]].[[organization]]
			AND [[organization_membership]].[[id]] = [[visibility]].[[organization_membership_id]]
			AND [[organization_membership]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_membership]].[[user]] = {:principalId}
			AND [[organization_membership]].[[status]] = {:activeStatus}
			AND [[organization_membership]].[[role]] IN ({:ownerRole}, {:adminRole}, {:securityRole}, {:billingRole}, {:memberRole}, {:guestRole})
			AND (([[workspace]].[[workspace_owner]] = {:principalId}
					AND [[visibility]].[[workspace_membership_id]] = ''
					AND [[visibility]].[[role]] = {:ownerRole})
				OR ([[workspace]].[[workspace_owner]] != {:principalId}
					AND [[visibility]].[[workspace_membership_id]] = {:principalId}
					AND [[visibility]].[[role]] = 'member'
					AND EXISTS (
						SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_membership]]
						WHERE [[workspace_membership]].[[workspace_id]] = [[workspace]].[[id]]
							AND [[workspace_membership]].[[user_id]] = {:principalId}
							AND [[workspace_membership]].[[role]] = 'member'
					)))
			AND (
				{:afterCreated} = '' OR [[visibility]].[[workspace_created]] > {:afterCreated}
				OR ([[visibility]].[[workspace_created]] = {:afterCreated} AND [[visibility]].[[workspace_id]] > {:afterId})
			)
		ORDER BY [[visibility]].[[workspace_created]], [[visibility]].[[workspace_id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"principalId": principalID, "activeStatus": organizationMemberStatusActive,
		"ownerRole": organizationRoleOwner, "adminRole": organizationRoleAdmin,
		"securityRole": organizationRoleSecurityAdmin, "billingRole": organizationRoleBillingAdmin,
		"memberRole": organizationRoleMember, "guestRole": organizationRoleGuest,
		"afterCreated": cursor.WorkspaceCreated,
		"afterId":      cursor.WorkspaceID, "limit": limit + 1,
	}).All(&rows)
	if err != nil {
		return nil, false, err
	}
	done := len(rows) <= limit
	if !done {
		rows = rows[:limit]
	}
	return rows, done, nil
}

// bootstrapPageColumnsSQL reads a bootstrap page and the reader's favorite
// of it, from page and favorite_state.
// pageHasContentSQL says whether a page's body holds any block — an empty
// line or a sub-page counts — which its icon shows, as Notion's (seen
// 2026-10-01): an empty document for none, a lined one otherwise.
const pageHasContentSQL = `EXISTS (
				SELECT 1 FROM {{leafen_blocks}} [[content_block]] INDEXED BY [[idx_leafen_blocks_page_order]]
				WHERE [[content_block]].[[owner]] = [[page]].[[owner]] AND [[content_block]].[[page_id]] = [[page]].[[id]]
			)`

var bootstrapPageColumnsSQL = `[[page]].[[id]], [[page]].[[workspace]], [[page]].[[teamspace_id]], [[page]].[[title]], [[page]].[[content]],
			[[page]].[[revision]], [[page]].[[cursor]], [[page]].[[parent_id]], [[page]].[[position]],
			[[page]].[[is_locked]], [[page]].[[locked_by]],
			[[page]].[[in_trash]], [[page]].[[trashed_at]], [[page]].[[archived_at]], [[page]].[[archived_by]],
			[[page]].[[icon]], [[page]].[[icon_type]],
			[[page]].[[cover]], [[page]].[[cover_position]], [[page]].[[font]],
			[[page]].[[small_text]], [[page]].[[full_width]],
			[[page]].[[backlinks_display]], [[page]].[[page_comments_display]], [[page]].[[table_of_contents_display]], [[page]].[[inline_comments_display]], [[page]].[[kind]],
			[[page]].[[created_by]], [[page]].[[last_edited_by]], [[page]].[[updated]], [[page]].[[created]],
			[[page]].[[wiki_revision]], [[page]].[[verified_at]], [[page]].[[verified_by]], [[page]].[[verification_expires_at]],
			COALESCE([[favorite_state]].[[favorite]], 0) AS [[favorite]],
			COALESCE([[favorite_state]].[[revision]], 0) AS [[favorite_revision]],
			` + databasePageSignpostSQL + ` AS [[moved_database]],
			` + viewBlockDatabasePageSQL + ` AS [[view_block]],
			` + fullPageDatabaseContainerSQL + ` AS [[database_container_id]],
			` + fullPageDatabaseBlockSQL + ` AS [[database_block_id]],
			` + pageHasContentSQL + ` AS [[has_content]]`

// bootstrapPageCompleteSQL holds back a page that a duplication is still
// making.
const bootstrapPageCompleteSQL = `(
				[[page]].[[duplication_target_job_id]] = '' OR ` + pageDuplicationCopyOpenSQL + ` OR EXISTS (
					SELECT 1 FROM {{leafen_page_duplication_jobs}} [[duplication]]
					JOIN {{leafen_jobs}} [[duplication_job]]
						ON [[duplication_job]].[[id]] = [[duplication]].[[job_id]]
					WHERE [[duplication]].[[job_id]] = [[page]].[[duplication_target_job_id]]
						AND [[duplication]].[[phase]] = 'completed'
						AND [[duplication_job]].[[status]] = 'completed'
				)
			)`

// workspaceMemberCountSQL counts the people of a workspace — its owner and
// members — as every place that names them counts ("N명의 멤버": the workspace
// menu, the account deletion card, the server's workspace list, a workspace one
// may join). An app's account is no person: neither as a member nor as the
// owner of the app's own workspace (app_platform_workspace.go).
func workspaceMemberCountSQL(workspace string) string {
	return `(SELECT COUNT(*) FROM {{leafen_workspaces}} [[counted_workspace]]
				WHERE [[counted_workspace]].[[id]] = ` + workspace + `
					AND NOT ` + integrationAccountSQL("[[counted_workspace]].[[workspace_owner]]") + `)
			+ (SELECT COUNT(*) FROM {{leafen_workspace_memberships}} [[workspace_member]]
				WHERE [[workspace_member]].[[workspace_id]] = ` + workspace + `
					AND NOT ` + integrationAccountSQL("[[workspace_member]].[[user_id]]") + `)`
}

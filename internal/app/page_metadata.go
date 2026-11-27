package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func updatePageMetadata(event *core.RequestEvent) error {
	return updatePageMetadataWithRooms(event, nil, nil)
}

// updatePageMetadataWithRooms applies a page metadata change from the app;
// a lock change also reconnects the page's open editors so they are
// authorized under the new lock, and a page moved or restored into a wiki
// becomes the wiki database's page with the pages under it.
func updatePageMetadataWithRooms(
	event *core.RequestEvent, rooms workspaceRoomInvalidator, ygo *pocketBaseYgoPersistence,
) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("id"))
	var input pageMetadataInput
	// What the request leaves out stays as the page has it
	// (page_metadata_sent_fields.go).
	var sent pageMetadataSentFields
	if err := rereadableRequestBody(event); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, refusalMessage("Check the page details.", bodyRefusalReason("page change", err)))
	}
	bindErr := event.BindBody(&input)
	if bindErr == nil {
		bindErr = event.BindBody(&sent)
	}
	if bindErr != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, refusalMessage("Check the page details.", bodyRefusalReason("page change", bindErr)))
	}
	input = normalizePageMetadataInput(input)
	input, sent = pageMetadataInputSpellingTitle(input, sent)
	// A title as its writer read it is the title stored (page_name_mentions.go),
	// its spans too (page_title_spans.go).
	if sent.Title != nil {
		input = pageTitleInputAsStored(event.App, user.Id, pageID, input)
	}
	if !validPageMetadataMutationInput(pageID, input) {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, refusalMessage("Check the page details.", pageMetadataInputProblem(pageID, input)))
	}

	// The list the change was made on goes beside it (page_metadata_list_token.go).
	listToken := input.WorkspaceRefreshToken
	input.WorkspaceRefreshToken = ""
	requestHash := pageMetadataSentRequestHash(pageID, input, sent)
	var (
		response  pageMetadataResponse
		status    pageMetadataMutationStatus
		refreshed string
	)
	storeRoomTypingBeforeLock(event, pageID, input.IsLocked != nil)
	err := event.App.RunInTransaction(func(txApp core.App) error {
		input = pageMetadataInputKeepingUnsent(txApp, pageID, input, sent)
		var err error
		response, status, refreshed, err = applyPageMetadataKeepingList(txApp, user, pageID, input, requestHash, listToken)
		return err
	})
	response.WorkspaceRefreshToken = refreshed
	if status == pageMetadataParentConflict {
		return answerPageParentRefused(event, user.Id, pageID, input)
	}
	if status != pageMetadataApplied {
		return answerPageMetadataRefused(event, user.Id, pageID, status)
	}
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "page-metadata")
	}
	if err != nil {
		var requestError *graphRequestError
		if errors.As(err, &requestError) {
			return apiError(event, requestError.status, requestError.code, requestError.message)
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't save the page details.", err)
	}
	if input.IsLocked != nil && !response.Receipt.Duplicate {
		invalidateLockedPageRoom(rooms, pageID)
	}
	// A full-page database's name follows its page's title; a replayed
	// change names it too, if it didn't the first time.
	if ygo != nil {
		if err := ygo.nameFullPageDatabase(event.Request.Context(), user.Id, pageID, input.TitleEdited, input.Icon != nil); err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed,
				"Couldn't save the page details.", "page_metadata", "database_name", errorCodePageMetadataUpdateFailed, err)
		}
	}
	if ygo != nil {
		adopted, err := ygo.adoptPageTreeIntoWiki(event.Request.Context(), user.Id, pageID)
		if err != nil {
			return apiErrorWithCause(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed,
				"Couldn't save the page details.", "page_metadata", "wiki", errorCodePageMetadataUpdateFailed, err)
		}
		if adopted {
			page, err := event.App.FindRecordById(pagesCollection, pageID)
			if err == nil {
				response.Page, err = pageRecordResponseWithWiki(event.App, page)
			}
			if err != nil {
				return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageMetadataUpdateFailed, "Couldn't save the page details.", err)
			}
		}
	}
	if ygo != nil && !input.InTrash && !response.Receipt.Duplicate {
		// Restoring an undone page conversion may have admitted a body move.
		ygo.notifyJobsAfterCommit()
	}
	// The page's title as its writer reads it (page_name_mentions.go).
	pageResponseAsRead(event.App, user.Id, &response.Page)
	return event.JSON(http.StatusOK, response)
}

type pageMetadataMutationStatus uint8

const (
	pageMetadataApplied pageMetadataMutationStatus = iota
	pageMetadataPageMissing
	pageMetadataParentConflict
	pageMetadataCursorConflict
	pageMetadataIdentityReused
	pageMetadataReplacementLocked
	pageMetadataPageLocked
)

// normalizePageMetadataInput tidies a metadata change's identifiers; its
// title is kept as written, spaces around it too, as Notion keeps a page's
// title typed in its app or written through its API (seen 2026-11-13: "앱제목
// 끝 " and " 바꾼 끝 " read back so): a change that only carries the title
// the page has — a move, a trash, an icon — leaves it as it is.
func normalizePageMetadataInput(input pageMetadataInput) pageMetadataInput {
	input.ParentID = strings.TrimSpace(input.ParentID)
	if input.TeamspaceID != nil {
		teamspaceID := strings.TrimSpace(*input.TeamspaceID)
		input.TeamspaceID = &teamspaceID
	}
	input.MutationID = strings.TrimSpace(input.MutationID)
	trimPagePresentationInput(&input)
	return input
}

func validPageMetadataMutationInput(pageID string, input pageMetadataInput) bool {
	return pageID != "" && len([]rune(input.Title)) <= maxGraphTitleRunes &&
		len(input.ParentID) <= maxWorkspacePermissionIdentifierLength && input.ParentID != pageID &&
		(input.TeamspaceID == nil || len(*input.TeamspaceID) <= maxWorkspacePermissionIdentifierLength) &&
		input.MutationID != "" && len([]rune(input.MutationID)) <= 255 && input.BaseCursor >= 0 &&
		validPageTitleSpansInput(input) && validPagePresentationInput(input) && validPageStyleMetadataInput(input) &&
		validPageCustomizationMetadataInput(input) && validPagePositionInput(input.Position)
}

func applyPageMetadata(
	app core.App,
	actorID string,
	pageID string,
	input pageMetadataInput,
	requestHash string,
) (pageMetadataResponse, pageMetadataMutationStatus, error) {
	return applyPageMetadataWithLock(app, actorID, pageID, input, requestHash, pageLockIgnored)
}

func applyPageMetadataWithLock(
	app core.App,
	actorID string,
	pageID string,
	input pageMetadataInput,
	requestHash string,
	lock pageLockMode,
) (pageMetadataResponse, pageMetadataMutationStatus, error) {
	response := pageMetadataResponse{}
	status := pageMetadataApplied
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		response, status, err = applyPageMetadataTransactionWithLock(
			txApp, personAccessSubject(actorID), pageID, input, requestHash, lock,
		)
		return err
	})
	return response, status, err
}

// applyPageMetadataTransaction changes page metadata for the public API, MCP
// and the server's own work, which the page lock does not stop.
func applyPageMetadataTransaction(
	app core.App,
	actorID, pageID string,
	input pageMetadataInput,
	requestHash string,
) (pageMetadataResponse, pageMetadataMutationStatus, error) {
	return applyPageMetadataTransactionWithLock(app, personAccessSubject(actorID), pageID, input, requestHash, pageLockIgnored)
}

func applyPageMetadataTransactionWithLock(
	app core.App,
	subject pageAccessSubject,
	pageID string,
	input pageMetadataInput,
	requestHash string,
	lock pageLockMode,
) (pageMetadataResponse, pageMetadataMutationStatus, error) {
	actorID := subject.actorID()
	authorization, err := subject.authorizePage(app, pageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return pageMetadataResponse{}, pageMetadataPageMissing, nil
	}
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	receipts, err := app.FindRecordsByFilter(
		receiptsCollection,
		"owner = {:owner} && mutation_id = {:mutationId}",
		"",
		1,
		0,
		dbx.Params{"owner": actorID, "mutationId": input.MutationID},
	)
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	if len(receipts) == 1 {
		receipt := receipts[0]
		if receipt.GetString("page") != pageID || receipt.GetString("request_hash") != requestHash && !pageMetadataTakenAlong(receipt) {
			return pageMetadataResponse{}, pageMetadataIdentityReused, nil
		}
		pageResponse, err := pageRecordResponseWithWiki(app, authorization.Page)
		if err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
		return pageMetadataResponse{
			Page: pageResponse,
			Receipt: pageMetadataReceipt{
				MutationID: input.MutationID, Cursor: receipt.GetInt("cursor"), Duplicate: true,
			},
		}, pageMetadataApplied, nil
	}
	leased, err := workspaceMarkdownReplacePageLeased(app, pageID, "")
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	if leased {
		return pageMetadataResponse{}, pageMetadataReplacementLocked, nil
	}

	page := authorization.Page
	oldParentID := page.GetString("parent_id")
	wasLocked, wasTrashed := page.GetBool("is_locked"), page.GetBool("in_trash")
	if holds, err := pageMetadataBaseHolds(app, actorID, page, input); err != nil || !holds {
		return pageMetadataResponse{}, pageMetadataCursorConflict, err
	}
	// Its content's rules: DLP, and pictures for its look (page_metadata_content_checks.go).
	if err := checkPageMetadataContent(app, authorization, page, input); err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	if lock == pageLockEnforced && pageMetadataChangesLockedContent(page, input) {
		locked, err := pageLockedFor(app, page, actorID)
		if err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
		if locked {
			return pageMetadataResponse{}, pageMetadataPageLocked, nil
		}
	}
	// The parent the request asks for; a restore that finds its parent gone
	// puts the page at the top, a place its trash chose, not a move.
	parentRequested := input.ParentID != page.GetString("parent_id")
	input, err = placePageMetadataInputAgainstTrash(app, page, input)
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	// Where the page goes, unless the place refuses it
	// (page_metadata_place.go says why).
	destination, refusal, err := pageMetadataPlaceRefusal(app, subject, authorization, input, parentRequested, lock)
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	if refusal != nil {
		return pageMetadataResponse{}, pageMetadataParentConflict, nil
	}
	placeChanges := destination != pagePlaceOf(page)
	previousParentID := page.GetString("parent_id")
	movementRequested := input.Position != nil || input.ParentID != previousParentID || placeChanges
	committed, err := commitPageMetadataRecordKept(
		app, authorization, actorID, pageMetadataKeepsLastEdit(page, input),
		func(page *core.Record, editedAt int64) {
			setPageTitle(page, input.Title, input.TitleSpans)
			page.Set("parent_id", input.ParentID)
			if input.Position != nil {
				page.Set("position", *input.Position)
			}
			if input.IsLocked != nil {
				page.Set("is_locked", *input.IsLocked)
				page.Set("locked_by", pageLockedBy(wasLocked, *input.IsLocked, page.GetString("locked_by"), actorID))
			}
			applyPageTrashState(page, input.InTrash, time.UnixMilli(editedAt).UTC().Format(time.RFC3339Nano))
			if input.IsArchived != nil {
				applyPageArchiveState(
					page, *input.IsArchived, actorID, time.UnixMilli(editedAt).UTC().Format(time.RFC3339Nano),
				)
			}
			if input.Icon != nil {
				page.Set("icon", *input.Icon)
				page.Set("icon_type", *input.IconType)
			}
			applyPageCover(page, input.Cover, input.CoverPosition)
			applyPageStyleMetadata(page, input)
			applyPageCustomizationMetadata(page, input)
		},
	)
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	page = committed.Page
	if placeChanges {
		if err := movePageTreeToPlace(
			app, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID, pageID, destination,
		); err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
		if input.ParentID == "" && destination.TeamspaceID == "" {
			if err := unsharePrivatePage(
				app, authorization.Workspace.WorkspaceID, pageID, actorID, time.UnixMilli(committed.EditedAt),
			); err != nil {
				return pageMetadataResponse{}, pageMetadataApplied, err
			}
		}
		if page, err = app.FindRecordById(pagesCollection, pageID); err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
	}
	// Subscriptions learn of a page sent to the trash, back from it, or
	// moved; of a full-page database as a database.
	switch {
	case page.GetBool("in_trash") != wasTrashed:
		kind, database := eventWebhookPageDeleted, eventWebhookDatabaseDeleted
		if wasTrashed {
			kind, database = eventWebhookPageUndeleted, eventWebhookDatabaseUndeleted
		}
		if isFullPageDatabase(page) {
			recordFullPageDatabaseWebhookEvent(app, database, page, actorID)
		} else {
			recordPageWebhookEvent(app, kind, page, actorID)
		}
	case page.GetString("parent_id") != previousParentID || placeChanges:
		if isFullPageDatabase(page) {
			recordFullPageDatabaseWebhookEvent(app, eventWebhookDatabaseMoved, page, actorID)
		} else {
			recordPageWebhookEvent(app, eventWebhookPageMoved, page, actorID)
		}
	}
	// Sockets open on the page, or under it, turn read-only or editable.
	if page.GetBool("in_trash") != wasTrashed || movementRequested {
		recheckCollaborationAccessAfterCommit(app)
	}
	// Pages showing its databases learn it went to the trash or came back.
	if page.GetBool("in_trash") != wasTrashed {
		if err := announcePageTreeGraphReadersAfterCommit(app, pageID); err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
	}
	if page.GetBool("is_locked") != wasLocked {
		if err := clearPageLockUnlocks(app, pageID); err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
		if err := recordPageLockChange(app, pageID, "", actorID, !wasLocked); err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
		kind := eventWebhookPageLocked
		if wasLocked {
			kind = eventWebhookPageUnlocked
		}
		recordPageWebhookEvent(app, kind, page, actorID)
	}
	if movementRequested && input.Position == nil && input.ParentID != oldParentID {
		if err := placePageLastAmongSiblings(
			app, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID, pageID, input.ParentID,
		); err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
		if refreshed, err := app.FindRecordById(pagesCollection, pageID); err == nil {
			page = refreshed
		} else {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
	}
	if err := keepChildPageBlockWithPage(
		app, authorization.Workspace.OwnerID, authorization.Workspace.WorkspaceID, actorID,
		page, oldParentID, wasTrashed, input.Position,
	); err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	// One back from the trash answers with the sidebar place its block
	// gave it (restoreChildPageBlock).
	if wasTrashed && !page.GetBool("in_trash") {
		if page, err = app.FindRecordById(pagesCollection, pageID); err != nil {
			return pageMetadataResponse{}, pageMetadataApplied, err
		}
	}
	if err := recordNotionAdminLegalHoldPageInteraction(app, pageEditedInteraction(
		"page-metadata", pageID+"\x00"+actorID+"\x00"+input.MutationID,
		authorization.Workspace.Organization.OrganizationID,
		authorization.Workspace.WorkspaceID, pageID, actorID, committed.EditedAt,
	)); err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	if err := replaceProductFilePageReferences(
		app, authorization.Workspace.WorkspaceID, page.Id,
		page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover"),
	); err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	receiptCollection, err := app.FindCollectionByNameOrId(receiptsCollection)
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	receipt := core.NewRecord(receiptCollection)
	receipt.Set("owner", actorID)
	receipt.Set("page", page.Id)
	receipt.Set("mutation_id", input.MutationID)
	receipt.Set("request_hash", requestHash)
	receipt.Set("cursor", committed.Cursor)
	if err := savePageMetadataReceipts(app, receipt, input); err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	pageResponse, err := pageRecordResponseWithWiki(app, page)
	if err != nil {
		return pageMetadataResponse{}, pageMetadataApplied, err
	}
	return pageMetadataResponse{
		Page: pageResponse,
		Receipt: pageMetadataReceipt{
			MutationID: input.MutationID, Cursor: committed.Cursor, Duplicate: false,
		},
	}, pageMetadataApplied, nil
}

// pageParentCreatesCycle reports whether putting a page under a parent puts
// it under itself, following the parent's ancestry through database pages to
// the pages above them.
func pageParentCreatesCycle(app core.App, pageID string, parentID string) (bool, error) {
	return pageAncestryIncludes(app, parentID, pageID)
}

func pageMetadataRequestHash(pageID string, input pageMetadataInput) string {
	return pageMetadataSentRequestHash(pageID, input, pageMetadataSentFields{
		Title: &input.Title, ParentID: &input.ParentID, InTrash: &input.InTrash,
	})
}

// pageMetadataSentRequestHash is a change's request hash, which names the
// always-written fields its request left out; one that sent them all hashes
// as it always did.
func pageMetadataSentRequestHash(pageID string, input pageMetadataInput, sent pageMetadataSentFields) string {
	payload := "metadata\x00" + pageID + "\x00" + strconv.Itoa(input.BaseCursor) + "\x00" + input.Title +
		"\x00" + input.ParentID + "\x00" + strconv.FormatBool(input.InTrash)
	if input.TitleEdited {
		payload += "\x00title-edited-v1"
	}
	payload += pageTitleSpansRequestHashInput(input)
	if input.Icon != nil || input.IconType != nil || input.Cover != nil || input.CoverPosition != nil {
		payload += "\x00presentation-v1\x00" + pageMetadataOptionalStringHash(input.Icon) +
			pageMetadataOptionalStringHash(input.IconType) + pageMetadataOptionalStringHash(input.Cover) +
			pageMetadataOptionalIntHash(input.CoverPosition)
	}
	if input.Position != nil {
		payload += "\x00position-v1\x00" + pageMetadataOptionalFloatHash(input.Position)
	}
	if input.TeamspaceID != nil {
		payload += "\x00place-v1\x00" + pageMetadataOptionalStringHash(input.TeamspaceID)
	}
	if input.IsLocked != nil {
		payload += "\x00lock-v1\x00" + pageMetadataOptionalBoolHash(input.IsLocked)
	}
	if input.IsArchived != nil {
		payload += "\x00archive-v1\x00" + pageMetadataOptionalBoolHash(input.IsArchived)
	}
	if input.Font != nil || input.SmallText != nil || input.FullWidth != nil {
		payload += "\x00style-v1\x00" + pageMetadataOptionalStringHash(input.Font) +
			pageMetadataOptionalBoolHash(input.SmallText) + pageMetadataOptionalBoolHash(input.FullWidth)
	}
	if input.BacklinksDisplay != nil || input.PageCommentsDisplay != nil {
		payload += "\x00customization-v1\x00" + pageMetadataOptionalStringHash(input.BacklinksDisplay) +
			pageMetadataOptionalStringHash(input.PageCommentsDisplay)
	}
	payload += pageCustomizationRequestHashInput(input) + sent.leftOut()
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func trimPagePresentationInput(input *pageMetadataInput) {
	for _, value := range []**string{&input.Icon, &input.IconType, &input.Cover} {
		if *value == nil {
			continue
		}
		normalized := strings.TrimSpace(**value)
		*value = &normalized
	}
}

func validPagePresentationInput(input pageMetadataInput) bool {
	if (input.Icon == nil) != (input.IconType == nil) {
		return false
	}
	if input.Icon != nil {
		if len([]rune(*input.Icon)) > maxPageIconRunes || len([]rune(*input.IconType)) > maxPageIconType {
			return false
		}
		switch *input.IconType {
		case "none":
			if *input.Icon != "" {
				return false
			}
		case "emoji", "image", "custom_emoji":
			if *input.Icon == "" {
				return false
			}
		case nativeIconType:
			if !validNativeIconValue(*input.Icon) {
				return false
			}
		default:
			return false
		}
	}
	if input.Cover != nil && len([]rune(*input.Cover)) > maxPageCoverRunes {
		return false
	}
	return input.CoverPosition == nil || (*input.CoverPosition >= 0 && *input.CoverPosition <= 100)
}

func validPagePositionInput(position *float64) bool {
	return position == nil || (!math.IsNaN(*position) && !math.IsInf(*position, 0))
}

func pageMetadataOptionalStringHash(value *string) string {
	if value == nil {
		return "n;"
	}
	return "v" + strconv.Itoa(len(*value)) + ":" + *value + ";"
}

func pageMetadataOptionalIntHash(value *int) string {
	if value == nil {
		return "n;"
	}
	encoded := strconv.Itoa(*value)
	return "v" + strconv.Itoa(len(encoded)) + ":" + encoded + ";"
}

func pageMetadataOptionalFloatHash(value *float64) string {
	if value == nil {
		return "n;"
	}
	encoded := strconv.FormatFloat(*value, 'g', -1, 64)
	return "v" + strconv.Itoa(len(encoded)) + ":" + encoded + ";"
}

func pageMetadataOptionalBoolHash(value *bool) string {
	if value == nil {
		return "n;"
	}
	return "v" + strconv.FormatBool(*value) + ";"
}

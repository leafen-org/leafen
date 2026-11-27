package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Emoji reactions on a page itself, as Notion's feed posts take them (seen
// 2026-10-11): a person reacts with an emoji once and reacting with it again
// takes it back, as on a comment; the page shows them over its comments.
// Anyone who can comment on the page can react, and a reaction moves the
// page's comment revision so the people reading its comments see it. The
// page's reactions are resolved together (schema_baseline.json).

type storedPageReaction struct {
	UserID string `db:"user_id"`
	Emoji  string `db:"emoji"`
}

func registerPageReactionRoutes(event *core.ServeEvent) {
	event.Router.Route(pageReactionSetMethod, apiRoot+pageReactionSetPath, commentsOfRowPage(
		refuseCommentWritesInTrash(setPageReactionRoute), refuseCommentWritesInTrash(setDatabaseRowPageReactionRoute),
	)).Bind(apis.BodyLimit(1024))
	event.Router.Route(databaseRowPageReactionSetMethod, apiRoot+databaseRowPageReactionSetPath, refuseCommentWritesInTrash(setDatabaseRowPageReactionRoute)).
		Bind(apis.BodyLimit(1024))
	event.Router.Route(pageReactionResolveMethod, apiRoot+pageReactionResolvePath, commentsOfRowPage(
		refuseCommentWritesInTrash(resolvePageReactionsRoute), refuseCommentWritesInTrash(resolveDatabaseRowPageReactionsRoute),
	)).Bind(apis.BodyLimit(1024))
	event.Router.Route(databaseRowPageReactionResolveMethod, apiRoot+databaseRowPageReactionResolvePath, refuseCommentWritesInTrash(resolveDatabaseRowPageReactionsRoute)).
		Bind(apis.BodyLimit(1024))
}

func resolvePageReactionsRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	response, err := resolvePageReactionsForTarget(event.App, nativePageCommentTarget(pageID),
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeCommentPage(txApp, user.Id, pageID, permissionWorkspaceWrite, "")
		})
	if err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

func resolveDatabaseRowPageReactionsRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	target, authorize, ok, err := databaseRowCommentMutationTarget(event, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")))
	if !ok {
		return err
	}
	response, err := resolvePageReactionsForTarget(event.App, target, authorize)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

// resolvePageReactionsForTarget resolves a page's reactions together, for
// whoever may react there, and answers with what is left: none.
func resolvePageReactionsForTarget(
	app core.App, target nativeCommentTarget, authorize func(core.App) (commentAuthorization, error),
) (pageReactionResponse, error) {
	if !validCommentIdentifier(target.ID) || !validCommentIdentifier(target.StoragePageID) {
		return pageReactionResponse{}, errCommentInvalid
	}
	response := pageReactionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		if authorization.Target != target {
			return permissionDenied()
		}
		result, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_page_reactions}} SET [[resolved_at]]={:now}
			WHERE [[target_kind]]={:kind} AND [[target_id]]={:targetId} AND [[resolved_at]]=''
		`).Bind(dbx.Params{
			"now": time.Now().UTC().Format(time.RFC3339Nano), "kind": target.Kind, "targetId": target.ID,
		}).Execute()
		if err != nil {
			return err
		}
		resolved, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if resolved > 0 {
			if err := advanceCommentPageRevision(txApp, target.StoragePageID); err != nil {
				return err
			}
		}
		reactions, err := loadPageReactions(txApp, target)
		if err != nil {
			return err
		}
		response = pageReactionResponse{Reactions: reactions, Duplicate: resolved == 0}
		return nil
	})
	return response, err
}

func setPageReactionRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentReactionInput{}
	if event.BindBody(&input) != nil {
		return commentAPIError(event, errCommentInvalid, persistentWriteComment)
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	response, err := setPageReactionForTarget(event.App, user.Id, nativePageCommentTarget(pageID), input,
		func(txApp core.App) (commentAuthorization, error) {
			return authorizeCommentPage(txApp, user.Id, pageID, permissionWorkspaceWrite, "")
		})
	if err != nil {
		return commentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

func setDatabaseRowPageReactionRoute(event *core.RequestEvent) error {
	user, ok, err := commentActor(event)
	if !ok {
		return err
	}
	input := commentReactionInput{}
	if event.BindBody(&input) != nil {
		return databaseRowCommentAPIError(event, errCommentInvalid, persistentWriteComment)
	}
	target, authorize, ok, err := databaseRowCommentMutationTarget(event, user.Id, strings.TrimSpace(event.Request.PathValue("rowId")))
	if !ok {
		return err
	}
	response, err := setPageReactionForTarget(event.App, user.Id, target, input, authorize)
	if err != nil {
		return databaseRowCommentAPIError(event, err, persistentWriteComment)
	}
	return event.JSON(http.StatusOK, response)
}

// setPageReactionForTarget adds or takes back the actor's emoji on a page or
// a row's page and answers with all its reactions. Asking for what is already
// so changes nothing and answers as a duplicate.
func setPageReactionForTarget(
	app core.App,
	actorID string,
	target nativeCommentTarget,
	input commentReactionInput,
	authorize func(core.App) (commentAuthorization, error),
) (pageReactionResponse, error) {
	if !validCommentReactionEmoji(input.Emoji) || !validCommentIdentifier(target.ID) ||
		!validCommentIdentifier(target.StoragePageID) {
		return pageReactionResponse{}, errCommentInvalid
	}
	response := pageReactionResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorize(txApp)
		if err != nil {
			return err
		}
		if authorization.Target != target {
			return permissionDenied()
		}
		changed, err := storePageReaction(txApp, authorization.Page.Workspace.WorkspaceID, target, actorID, input)
		if err != nil {
			return err
		}
		if changed {
			if err := advanceCommentPageRevision(txApp, target.StoragePageID); err != nil {
				return err
			}
		}
		reactions, err := loadPageReactions(txApp, target)
		if err != nil {
			return err
		}
		response = pageReactionResponse{Reactions: reactions, Duplicate: !changed}
		return nil
	})
	return response, err
}

// storePageReaction writes one person's emoji on a page, or takes it out,
// and says whether anything changed, as a comment's reaction is kept.
func storePageReaction(
	app core.App, workspaceID string, target nativeCommentTarget, actorID string, input commentReactionInput,
) (bool, error) {
	params := dbx.Params{"kind": target.Kind, "targetId": target.ID, "userId": actorID, "emoji": input.Emoji}
	if !input.Reacted {
		result, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_page_reactions}}
			WHERE [[target_kind]]={:kind} AND [[target_id]]={:targetId} AND [[user_id]]={:userId} AND [[emoji]]={:emoji}
				AND [[resolved_at]]=''
		`).Bind(params).Execute()
		if err != nil {
			return false, err
		}
		removed, err := result.RowsAffected()
		return removed > 0, err
	}
	var own struct {
		Exists int `db:"existing"`
	}
	if err := app.DB().NewQuery(`
		SELECT COALESCE(SUM([[emoji]]={:emoji}),0) AS [[existing]]
		FROM {{leafen_page_reactions}}
		WHERE [[target_kind]]={:kind} AND [[target_id]]={:targetId} AND [[user_id]]={:userId} AND [[resolved_at]]=''
	`).Bind(params).One(&own); err != nil {
		return false, err
	}
	if own.Exists != 0 {
		return false, nil
	}
	if emojiID, custom := strings.CutPrefix(input.Emoji, commentReactionCustomEmojiPrefix); custom {
		var found int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_custom_emojis}} WHERE [[id]]={:emojiId} AND [[workspace_id]]={:workspaceId}
		`).Bind(dbx.Params{"emojiId": emojiID, "workspaceId": workspaceID}).Row(&found); err != nil {
			return false, err
		}
		if found == 0 {
			return false, errCommentInvalid
		}
	}
	// The same emoji resolved before starts again in the new group.
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_reactions}}
			([[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],[[user_id]],[[emoji]],[[created_at]])
		VALUES ({:workspaceId},{:pageId},{:kind},{:targetId},{:userId},{:emoji},{:now})
		ON CONFLICT ([[target_kind]],[[target_id]],[[user_id]],[[emoji]]) DO UPDATE SET
			[[created_at]]=excluded.[[created_at]],[[resolved_at]]=''
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pageId": target.StoragePageID, "kind": target.Kind, "targetId": target.ID,
		"userId": actorID, "emoji": input.Emoji, "now": time.Now().UTC().Format(time.RFC3339Nano),
	}).Execute()
	return err == nil, err
}

// loadPageReactions reads a page's reactions: each emoji with its people in
// the order they reacted, the emoji in the order each was first used.
func loadPageReactions(app core.App, target nativeCommentTarget) ([]commentReaction, error) {
	rows := []storedPageReaction{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]],[[emoji]] FROM {{leafen_page_reactions}}
		WHERE [[target_kind]]={:kind} AND [[target_id]]={:targetId} AND [[resolved_at]]=''
		ORDER BY [[created_at]],[[user_id]],[[emoji]]
	`).Bind(dbx.Params{"kind": target.Kind, "targetId": target.ID}).All(&rows); err != nil {
		return nil, err
	}
	reactions := []commentReaction{}
	for _, row := range rows {
		at := -1
		for index := range reactions {
			if reactions[index].Emoji == row.Emoji {
				at = index
				break
			}
		}
		if at < 0 {
			reactions = append(reactions, commentReaction{Emoji: row.Emoji, UserIDs: []string{}})
			at = len(reactions) - 1
		}
		reactions[at].UserIDs = append(reactions[at].UserIDs, row.UserID)
	}
	return reactions, nil
}

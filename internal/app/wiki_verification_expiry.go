package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	wikiVerificationQueueClaimLimit = 8
	wikiVerificationLeaseDuration   = 30 * time.Second

	wikiVerificationQueueReady      = "ready"
	wikiVerificationQueueClaimed    = "claimed"
	wikiVerificationQueueDelivering = "delivering"
	wikiVerificationQueueCompleted  = "completed"
	wikiVerificationQueueCancelled  = "cancelled"
	wikiVerificationQueueFailed     = "failed"
)

type storedWikiVerificationClaim struct {
	PageID string `db:"page_id"`
	// DataSourceID names the database on the page whose verification this is,
	// or is empty for the page's own.
	DataSourceID string `db:"data_source_id"`
	Generation   int    `db:"generation"`
	WorkspaceID  string `db:"workspace_id"`
	DueAt        int64  `db:"due_at"`
	Attempts     int    `db:"attempts"`
	LeaseToken   string `db:"lease_token"`
}

type storedWikiVerificationPage struct {
	Generation int    `db:"wiki_verification_generation"`
	ExpiresAt  string `db:"verification_expires_at"`
	VerifiedAt string `db:"verified_at"`
	Title      string `db:"title"`
	// BlockID is a database's block on its page, for the reminder's link.
	BlockID string `db:"block_id"`
}

type wikiVerificationRecipient struct {
	UserID string `db:"user_id"`
	Email  string `db:"email"`
}

func claimDueWikiVerifications(
	ctx context.Context,
	app core.App,
	now time.Time,
) ([]storedWikiVerificationClaim, bool, error) {
	claimed := []storedWikiVerificationClaim{}
	hasMore := false
	err := app.RunInTransaction(func(txApp core.App) error {
		released, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_wiki_verification_queue}}
			SET [[status]]='ready',[[lease_token]]='',[[lease_until]]=0,
				[[next_attempt_at]]=MIN([[next_attempt_at]],{:now}),[[updated_at]]={:now}
			WHERE [[rowid]] IN (
				SELECT [[rowid]] FROM {{leafen_wiki_verification_queue}}
				INDEXED BY [[idx_leafen_wiki_verification_claimed]]
				WHERE [[status]]='claimed' AND [[lease_until]] <= {:now}
				ORDER BY [[lease_until]],[[page_id]],[[data_source_id]],[[generation]] LIMIT {:limit}
			)
		`).WithContext(ctx).Bind(dbx.Params{
			"now": now.UnixMilli(), "limit": wikiVerificationQueueClaimLimit,
		}).Execute()
		if err != nil {
			return err
		}
		releasedCount, err := released.RowsAffected()
		if err != nil {
			return err
		}
		hasMore = releasedCount == wikiVerificationQueueClaimLimit
		candidates := []storedWikiVerificationClaim{}
		limit := wikiVerificationQueueClaimLimit + 1
		if err := txApp.DB().NewQuery(`
			SELECT [[page_id]],[[data_source_id]],[[generation]],[[workspace_id]],[[due_at]],[[attempts]],'' AS [[lease_token]]
			FROM {{leafen_wiki_verification_queue}}
			INDEXED BY [[idx_leafen_wiki_verification_ready]]
			WHERE [[status]]='ready' AND [[next_attempt_at]] <= {:now} AND [[due_at]] <= {:now}
			ORDER BY [[next_attempt_at]],[[due_at]],[[page_id]],[[data_source_id]],[[generation]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"now": now.UnixMilli(), "limit": limit,
		}).All(&candidates); err != nil {
			return err
		}
		candidateMore := len(candidates) > wikiVerificationQueueClaimLimit
		hasMore = hasMore || candidateMore
		if candidateMore {
			candidates = candidates[:wikiVerificationQueueClaimLimit]
		}
		for _, candidate := range candidates {
			token := core.GenerateDefaultRandomId()
			result, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_wiki_verification_queue}}
				SET [[status]]='claimed',[[attempts]]=[[attempts]]+1,
					[[lease_token]]={:token},[[lease_until]]={:leaseUntil},[[updated_at]]={:now}
				WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId} AND [[generation]]={:generation}
					AND [[status]]='ready'
			`).WithContext(ctx).Bind(dbx.Params{
				"token": token, "leaseUntil": now.Add(wikiVerificationLeaseDuration).UnixMilli(),
				"now": now.UnixMilli(), "pageId": candidate.PageID, "dataSourceId": candidate.DataSourceID,
				"generation": candidate.Generation,
			}).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if changed == 1 {
				candidate.Attempts++
				candidate.LeaseToken = token
				claimed = append(claimed, candidate)
			}
		}
		return nil
	})
	return claimed, hasMore, err
}

func expireWikiVerificationClaims(
	ctx context.Context,
	app core.App,
	claims []storedWikiVerificationClaim,
	now time.Time,
) (wikiVerificationTurn, error) {
	turn := wikiVerificationTurn{}
	errs := []error{}
	for _, claim := range claims {
		if err := ctx.Err(); err != nil {
			return turn, errors.Join(append(errs, err)...)
		}
		expired, notifications, cancelled, err := expireWikiVerificationClaim(ctx, app, claim, now)
		turn.Expired += expired
		turn.Notifications += notifications
		turn.Cancelled += cancelled
		if err != nil {
			errs = append(errs, err)
		}
	}
	return turn, errors.Join(errs...)
}

func expireWikiVerificationClaim(
	ctx context.Context,
	app core.App,
	claim storedWikiVerificationClaim,
	now time.Time,
) (expired int, notifications int, cancelled int, resultErr error) {
	resultErr = app.RunInTransaction(func(txApp core.App) error {
		page := storedWikiVerificationPage{}
		read := `
			SELECT [[wiki_verification_generation]],[[verification_expires_at]],[[verified_at]],[[title]],'' AS [[block_id]]
			FROM {{leafen_pages}} WHERE [[id]]={:pageId} AND [[workspace]]={:workspaceId}`
		if claim.DataSourceID != "" {
			// A database's reminder names the database, as its page shows it.
			read = `
			SELECT [[source]].[[verification_generation]] AS [[wiki_verification_generation]],
				[[source]].[[verification_expires_at]],[[source]].[[verified_at]],[[source]].[[title]],
				COALESCE((SELECT [[container]].[[block_id]] FROM {{leafen_database_container_sources}} [[membership]]
					JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[membership]].[[container_id]]
					WHERE [[membership]].[[data_source_id]] = [[source]].[[id]] AND [[container]].[[kind]] = 'source'
						AND [[container]].[[page_id]] = [[source]].[[page_id]]
					ORDER BY [[container]].[[id]] LIMIT 1), '') AS [[block_id]]
			FROM {{leafen_data_sources}} [[source]]
			WHERE [[source]].[[id]]={:dataSourceId} AND [[source]].[[page_id]]={:pageId}
				AND [[source]].[[workspace_id]]={:workspaceId}`
		}
		err := txApp.DB().NewQuery(read).WithContext(ctx).Bind(dbx.Params{
			"pageId": claim.PageID, "dataSourceId": claim.DataSourceID, "workspaceId": claim.WorkspaceID,
		}).One(&page)
		if errors.Is(err, sql.ErrNoRows) {
			return cancelClaimedWikiVerification(ctx, txApp, claim, now)
		}
		if err != nil {
			return err
		}
		due, dueErr := time.Parse(time.RFC3339Nano, page.ExpiresAt)
		if page.Generation != claim.Generation || page.VerifiedAt == "" || dueErr != nil || due.After(now) {
			if err := cancelClaimedWikiVerification(ctx, txApp, claim, now); err != nil {
				return err
			}
			cancelled = 1
			return nil
		}
		recipients, err := currentWikiVerificationRecipients(ctx, txApp, claim)
		if err != nil {
			return err
		}
		expire := `
			UPDATE {{leafen_pages}}
			SET [[verified_at]]='',[[verified_by]]='',[[verification_expires_at]]='',
				[[wiki_revision]]=[[wiki_revision]]+1
			WHERE [[id]]={:pageId} AND [[workspace]]={:workspaceId}
				AND [[wiki_verification_generation]]={:generation}
				AND [[verification_expires_at]]={:expiresAt} AND [[verified_at]] != ''`
		if claim.DataSourceID != "" {
			expire = `
			UPDATE {{leafen_data_sources}}
			SET [[verified_at]]='',[[verified_by]]='',[[verification_expires_at]]='',
				[[verification_revision]]=[[verification_revision]]+1
			WHERE [[id]]={:dataSourceId} AND [[workspace_id]]={:workspaceId}
				AND [[verification_generation]]={:generation}
				AND [[verification_expires_at]]={:expiresAt} AND [[verified_at]] != ''`
		}
		update, err := txApp.DB().NewQuery(expire).WithContext(ctx).Bind(dbx.Params{
			"pageId": claim.PageID, "dataSourceId": claim.DataSourceID, "workspaceId": claim.WorkspaceID,
			"generation": claim.Generation, "expiresAt": page.ExpiresAt,
		}).Execute()
		if err != nil {
			return err
		}
		changed, err := update.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return cancelClaimedWikiVerification(ctx, txApp, claim, now)
		}
		notifications, err = admitWikiVerificationRecipientEffects(
			ctx, txApp, claim, page.Title, page.BlockID, recipients, now,
		)
		if err != nil {
			return err
		}
		status := wikiVerificationQueueCompleted
		completedAt := now.UnixMilli()
		if len(recipients) > 0 {
			status = wikiVerificationQueueDelivering
			completedAt = 0
		}
		settled, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_wiki_verification_queue}}
			SET [[status]]={:status},[[lease_token]]='',[[lease_until]]=0,[[error_code]]='',
				[[expired_at]]={:now},[[completed_at]]={:completedAt},[[updated_at]]={:now}
			WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId} AND [[generation]]={:generation}
				AND [[status]]='claimed' AND [[lease_token]]={:token}
		`).WithContext(ctx).Bind(dbx.Params{
			"status": status, "now": now.UnixMilli(), "completedAt": completedAt,
			"pageId": claim.PageID, "dataSourceId": claim.DataSourceID, "generation": claim.Generation,
			"token": claim.LeaseToken,
		}).Execute()
		if err != nil {
			return err
		}
		rows, err := settled.RowsAffected()
		if err != nil || rows != 1 {
			return errors.Join(errWikiConflict, err)
		}
		expired = 1
		return nil
	})
	return
}

func currentWikiVerificationRecipients(
	ctx context.Context,
	app core.App,
	claim storedWikiVerificationClaim,
) ([]wikiVerificationRecipient, error) {
	recipients := []wikiVerificationRecipient{}
	// A wiki page's owners are its Owner values, the first of them as many
	// as a page may have owners; a database's are those it names.
	var owners sql.NullString
	err := sql.ErrNoRows
	if claim.DataSourceID == "" {
		err = app.DB().NewQuery(`
		SELECT [[value]].[[value]] FROM {{leafen_pages}} AS [[page]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[page]].[[data_source_id]]
			AND [[source]].[[wiki_verification_property_id]] != ''
		LEFT JOIN {{leafen_database_row_values}} AS [[value]]
			ON [[value]].[[row_id]] = [[page]].[[id]] AND [[value]].[[property_id]] = [[source]].[[wiki_owner_property_id]]
		WHERE [[page]].[[id]] = {:pageId}
	`).WithContext(ctx).Bind(dbx.Params{"pageId": claim.PageID}).Row(&owners)
	}
	if err == nil {
		ownerValue := "[]"
		if owners.Valid && json.Valid([]byte(owners.String)) {
			ownerValue = owners.String
		}
		err = app.DB().NewQuery(`
			SELECT [[owner]].[[value]] AS [[user_id]], [[user]].[[email]]
			FROM json_each({:owners}) AS [[owner]]
			JOIN {{users}} [[user]] ON [[user]].[[id]] = [[owner]].[[value]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
			WHERE [[owner]].[[type]] = 'text' AND (
				[[owner]].[[value]] = [[workspace]].[[workspace_owner]] OR EXISTS (
					SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
					WHERE [[membership]].[[workspace_id]] = {:workspaceId}
						AND [[membership]].[[user_id]] = [[owner]].[[value]]
						AND [[membership]].[[role]] = 'member'
				)
			)
			GROUP BY [[owner]].[[value]] ORDER BY MIN([[owner]].[[key]]) LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"owners": ownerValue, "workspaceId": claim.WorkspaceID, "limit": maxWikiOwnersPerPage,
		}).All(&recipients)
		return recipients, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	err = app.DB().NewQuery(`
		SELECT [[owner]].[[user_id]],[[user]].[[email]]
		FROM {{leafen_wiki_page_owners}} [[owner]]
		JOIN {{users}} [[user]] ON [[user]].[[id]]=[[owner]].[[user_id]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]]={:workspaceId}
		WHERE [[owner]].[[page_id]]={:pageId} AND [[owner]].[[data_source_id]]={:dataSourceId} AND (
			[[owner]].[[user_id]]=[[workspace]].[[workspace_owner]] OR EXISTS (
				SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
				WHERE [[membership]].[[workspace_id]]={:workspaceId}
					AND [[membership]].[[user_id]]=[[owner]].[[user_id]]
					AND [[membership]].[[role]]='member'
			)
		)
		ORDER BY [[owner]].[[position]],[[owner]].[[user_id]] LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{
		"workspaceId": claim.WorkspaceID, "pageId": claim.PageID, "dataSourceId": claim.DataSourceID,
		"limit": maxWikiOwnersPerPage + 1,
	}).All(&recipients)
	if err != nil || len(recipients) > maxWikiOwnersPerPage {
		return nil, errors.Join(errWikiConflict, err)
	}
	return recipients, nil
}

func cancelClaimedWikiVerification(
	ctx context.Context,
	app core.App,
	claim storedWikiVerificationClaim,
	now time.Time,
) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_wiki_verification_queue}}
		SET [[status]]='cancelled',[[lease_token]]='',[[lease_until]]=0,
			[[completed_at]]={:now},[[updated_at]]={:now}
		WHERE [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId} AND [[generation]]={:generation}
			AND [[status]]='claimed' AND [[lease_token]]={:token}
	`).WithContext(ctx).Bind(dbx.Params{
		"now": now.UnixMilli(), "pageId": claim.PageID, "dataSourceId": claim.DataSourceID,
		"generation": claim.Generation, "token": claim.LeaseToken,
	}).Execute()
	return err
}

func admitWikiVerificationRecipientEffects(
	ctx context.Context,
	app core.App,
	claim storedWikiVerificationClaim,
	pageTitle string,
	blockID string,
	recipients []wikiVerificationRecipient,
	now time.Time,
) (int, error) {
	if len(recipients) > maxWikiOwnersPerPage {
		return 0, errWikiConflict
	}
	// A database's notice names the database on its page; a page's keeps the
	// keys it always had. The target is the app's address for the page.
	subject, target := claim.PageID, "/p/"+url.PathEscape(claim.PageID)
	if claim.DataSourceID != "" {
		subject = claim.PageID + ":" + claim.DataSourceID
		if blockID != "" {
			target = "/p/" + url.PathEscape(blockID)
		}
	}
	activityKey := fmt.Sprintf("wiki-verification-expired:%s:%d", subject, claim.Generation)
	projections := make([]productNotificationProjection, 0, len(recipients))
	emailValues := make([]string, 0, len(recipients))
	emailParams := dbx.Params{
		"pageId": claim.PageID, "dataSourceId": claim.DataSourceID, "generation": claim.Generation,
		"workspaceId": claim.WorkspaceID, "title": pageTitle, "now": now.UnixMilli(),
	}
	for index, recipient := range recipients {
		projections = append(projections, productNotificationProjection{
			ID: wikiVerificationStableID(
				"notification", subject, fmt.Sprint(claim.Generation), recipient.UserID,
			),
			EventID:     wikiVerificationStableID("event", subject, fmt.Sprint(claim.Generation)),
			WorkspaceID: claim.WorkspaceID,
			UserID:      recipient.UserID,
			ActivityKey: activityKey,
			Kind:        "system",
			PageID:      claim.PageID,
			Title:       pageTitle,
			Preview:     "Wiki verification expired",
			Target:      target,
			OccurredAt:  now.UnixMilli(),
		})
		if strings.TrimSpace(recipient.Email) == "" {
			continue
		}
		prefix := fmt.Sprintf("email%d", index)
		emailParams[prefix+"Id"] = wikiVerificationEmailDeliveryID(
			subject, claim.Generation, recipient.UserID,
		)
		emailParams[prefix+"UserId"] = recipient.UserID
		emailParams[prefix+"Recipient"] = recipient.Email
		emailValues = append(emailValues, fmt.Sprintf(
			"({:%sId},{:pageId},{:dataSourceId},{:generation},{:workspaceId},{:%sUserId},{:%sRecipient},"+
				"{:title},'ready',0,{:now},'',0,'','',{:now},{:now},0)",
			prefix, prefix, prefix,
		))
	}
	inserted, err := upsertProductNotifications(ctx, app, projections, now.UnixMilli())
	if err != nil || len(emailValues) == 0 {
		return inserted, err
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_wiki_verification_email_deliveries}} (
			[[id]],[[page_id]],[[data_source_id]],[[generation]],[[workspace_id]],[[user_id]],[[recipient_email]],
			[[page_title]],[[status]],[[attempts]],[[next_attempt_at]],[[lease_token]],
			[[lease_until]],[[error_code]],[[provider_id]],[[created_at]],[[updated_at]],[[sent_at]]
		) VALUES ` + joinSQL(emailValues) + `
		ON CONFLICT ([[page_id]],[[data_source_id]],[[generation]],[[user_id]]) DO NOTHING
	`).WithContext(ctx).Bind(emailParams).Execute()
	return inserted, err
}

// wikiVerificationEmailDeliveryID names a reminder by its subject: the page,
// or "page:container" for a database.
func wikiVerificationEmailDeliveryID(subject string, generation int, userID string) string {
	return wikiVerificationStableID("email", subject, fmt.Sprint(generation), userID)
}

func wikiVerificationStableID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:16])
}

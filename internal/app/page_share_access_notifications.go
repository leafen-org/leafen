package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const pageShareAccessNoticeWindow = 32

type storedPageShareAccessPending struct {
	PageID      string `db:"page_id"`
	PrincipalID string `db:"principal_id"`
	WorkspaceID string `db:"workspace_id"`
	ObservedAt  int64  `db:"observed_at"`
	CursorID    string `db:"cursor_id"`
}

// Only identified, authorized UI observations enter this durable queue. API
// background reads do not announce a person's visit. The worker rechecks ACL
// and privacy before disclosing it, and advances a bounded window per turn.
func queuePageShareAccessNotices(app core.App, workspaceID, userID string, pageIDs []string, now time.Time) error {
	if len(pageIDs) == 0 {
		return nil
	}
	// Where the organization names no one as having viewed, no one is told.
	if display, err := workspaceViewDisplay(app, workspaceID); err != nil || display == viewDisplayHidden {
		return err
	}
	principals, err := loadPageSharePrincipals(app, workspaceID, userID)
	if err != nil {
		return err
	}
	params := dbx.Params{"workspace": workspaceID, "user": userID, "observed": now.UnixMilli()}
	condition := principals.condition("[[permission]]", "workspace", params)
	values := make([]string, 0, len(pageIDs))
	for i, id := range pageIDs {
		key := fmt.Sprintf("visited%d", i)
		params[key] = id
		values = append(values, "({:"+key+"})")
	}
	_, err = app.DB().NewQuery(`WITH [[visited]] ([[page_id]]) AS (VALUES ` + joinSQL(values) + `)
 INSERT INTO {{leafen_page_share_access_pending}} ([[page_id]],[[principal_id]],[[workspace_id]],[[observed_at]],[[cursor_id]])
 SELECT [[visited]].[[page_id]],{:user},{:workspace},{:observed},'' FROM [[visited]]
 WHERE EXISTS (SELECT 1 FROM {{leafen_page_permissions}} [[permission]]
  WHERE [[permission]].[[page_id]]=[[visited]].[[page_id]] AND [[permission]].[[notify_on_access]]=1
  AND [[permission]].[[principal_type]]!='workspace' AND [[permission]].[[created_by]]!={:user}
  AND ` + condition + ` AND NOT EXISTS (SELECT 1 FROM {{leafen_page_share_access_receipts}} [[receipt]]
   WHERE [[receipt]].[[share_id]]=[[permission]].[[id]] AND [[receipt]].[[principal_id]]={:user}))
 ON CONFLICT ([[page_id]],[[principal_id]]) DO UPDATE SET [[observed_at]]=excluded.[[observed_at]],[[cursor_id]]=''`).Bind(params).Execute()
	return err
}

func advancePageShareAccessNotice(ctx context.Context, app core.App) (bool, error) {
	selected := storedPageShareAccessPending{}
	err := app.DB().NewQuery(`SELECT [[page_id]],[[principal_id]],[[workspace_id]],[[observed_at]],[[cursor_id]]
 FROM {{leafen_page_share_access_pending}} ORDER BY [[observed_at]],[[page_id]],[[principal_id]] LIMIT 1`).WithContext(ctx).One(&selected)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	release := acquireProductOperationLock("page-share-access", selected.PageID+"\x00"+selected.PrincipalID)
	defer release()
	err = app.RunInTransaction(func(tx core.App) error {
		pending := storedPageShareAccessPending{}
		params := dbx.Params{"page": selected.PageID, "user": selected.PrincipalID, "workspace": selected.WorkspaceID}
		err := tx.DB().NewQuery(`SELECT [[page_id]],[[principal_id]],[[workspace_id]],[[observed_at]],[[cursor_id]] FROM {{leafen_page_share_access_pending}}
   WHERE [[page_id]]={:page} AND [[principal_id]]={:user}`).WithContext(ctx).Bind(params).One(&pending)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		drop := func() error {
			_, err := tx.DB().NewQuery(`DELETE FROM {{leafen_page_share_access_pending}} WHERE [[page_id]]={:page} AND [[principal_id]]={:user}`).Bind(params).Execute()
			return err
		}
		// A page an operation holds a while is still theirs (page_access_hold.go): its notice isn't dropped.
		authorization, err := pageAccessDespiteHold(authorizePageAccessRole(tx, pending.PrincipalID, pending.PageID, pageAccessRoleView))
		if isPermissionDenied(err) {
			return drop()
		}
		if err != nil {
			return err
		}
		if authorization.Workspace.WorkspaceID != pending.WorkspaceID || !pageAnalyticsPolicy(authorization.Workspace.Workspace).AnalyticsEnabled {
			return drop()
		}
		privacyIDs := []string{pending.PageID}
		if isDatabaseRowPageRecord(authorization.Page) {
			containerID, err := databaseRowPageAbove(tx, authorization.Page)
			if err != nil {
				return err
			}
			privacyIDs = append(privacyIDs, containerID)
		}
		identified, err := pageAnalyticsIdentifiedPages(tx, pending.WorkspaceID, pending.PrincipalID, privacyIDs)
		if err != nil {
			return err
		}
		if len(identified) != len(privacyIDs) {
			return drop()
		}
		if display, err := workspaceViewDisplay(tx, pending.WorkspaceID); err != nil {
			return err
		} else if display == viewDisplayHidden {
			return drop()
		}
		principals, err := loadPageSharePrincipals(tx, pending.WorkspaceID, pending.PrincipalID)
		if err != nil {
			return err
		}
		condition := principals.condition("[[permission]]", "workspace", params)
		params["observed"], params["cursor"], params["limit"] = pending.ObservedAt, pending.CursorID, pageShareAccessNoticeWindow
		grants := []struct {
			ID        string `db:"id"`
			InviterID string `db:"created_by"`
		}{}
		if err := tx.DB().NewQuery(`SELECT [[permission]].[[id]],[[permission]].[[created_by]] FROM {{leafen_page_permissions}} [[permission]]
   WHERE [[permission]].[[page_id]]={:page} AND [[permission]].[[notify_on_access]]=1 AND [[permission]].[[principal_type]]!='workspace'
   AND [[permission]].[[created_at]]<={:observed} AND [[permission]].[[id]]>{:cursor} AND ` + condition + `
   AND NOT EXISTS (SELECT 1 FROM {{leafen_page_share_access_receipts}} [[receipt]] WHERE [[receipt]].[[share_id]]=[[permission]].[[id]] AND [[receipt]].[[principal_id]]={:user})
   ORDER BY [[permission]].[[id]] LIMIT {:limit}`).WithContext(ctx).Bind(params).All(&grants); err != nil {
			return err
		}
		notifications := []productNotificationProjection{}
		target, err := pageNotificationTarget(tx, pending.PageID)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		for _, grant := range grants {
			params["share"] = grant.ID
			result, err := tx.DB().NewQuery(`INSERT INTO {{leafen_page_share_access_receipts}} ([[share_id]],[[principal_id]],[[workspace_id]],[[page_id]],[[observed_at]])
    VALUES ({:share},{:user},{:workspace},{:page},{:observed}) ON CONFLICT ([[share_id]],[[principal_id]]) DO NOTHING`).Bind(params).Execute()
			if err != nil {
				return err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if changed == 0 || grant.InviterID == pending.PrincipalID {
				continue
			}
			if _, err := pageAccessDespiteHold(authorizePageAccessRole(tx, grant.InviterID, pending.PageID, pageAccessRoleView)); isPermissionDenied(err) {
				continue
			} else if err != nil {
				return err
			}
			digest := sha256.Sum256([]byte("page-share-access\x00" + grant.ID + "\x00" + pending.PrincipalID))
			id := hex.EncodeToString(digest[:16])
			notifications = append(notifications, productNotificationProjection{ID: id, EventID: id, WorkspaceID: pending.WorkspaceID, UserID: grant.InviterID,
				ActivityKey: "page-share-access:" + grant.ID + ":" + pending.PrincipalID, Kind: "system", PageID: pending.PageID, ActorID: pending.PrincipalID,
				Title: authorization.Page.GetString("title"), Target: target, OccurredAt: pending.ObservedAt, Personal: true})
		}
		if _, err := upsertProductNotifications(ctx, tx, notifications, now); err != nil {
			return err
		}
		if len(grants) < pageShareAccessNoticeWindow {
			return drop()
		}
		params["cursor"] = grants[len(grants)-1].ID
		_, err = tx.DB().NewQuery(`UPDATE {{leafen_page_share_access_pending}} SET [[cursor_id]]={:cursor} WHERE [[page_id]]={:page} AND [[principal_id]]={:user}`).Bind(params).Execute()
		return err
	})
	return true, err
}

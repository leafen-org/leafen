package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedCanonicalActorProfile struct {
	UserID      string `db:"user_id"`
	DisplayName string `db:"display_name"`
	Avatar      string `db:"avatar"`
	Revision    int    `db:"revision"`
}

// readCanonicalActorProfiles projects only the identities already present in
// one bounded authorized response. The users record stays the sole writable
// authority; missing identities are omitted rather than replaced by a mirror.
// An app's own account is marked App: every screen naming who did something
// names it as a connection is — its name alone, never "〈name〉님", on a
// connection's face — as the page's history marks it "(앱)".
func readCanonicalActorProfiles(app core.App, actorIDs []string) ([]commentActorProfile, error) {
	unique := make(map[string]struct{}, len(actorIDs))
	for _, actorID := range actorIDs {
		if actorID != "" {
			unique[actorID] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(unique))
	for actorID := range unique {
		ordered = append(ordered, actorID)
	}
	sort.Strings(ordered)
	if len(ordered) > commentNotificationInboxMaxSize {
		return nil, errCommentFailed
	}
	profiles := []commentActorProfile{}
	if len(ordered) == 0 {
		return profiles, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, len(ordered))
	for index, actorID := range ordered {
		key := fmt.Sprintf("actor%d", index)
		params[key], placeholders[index] = actorID, "{:"+key+"}"
	}
	rows := []storedCanonicalActorProfile{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]] AS [[user_id]], [[user]].[[name]] AS [[display_name]],
			[[user]].[[profile_avatar]] AS [[avatar]], [[profile]].[[revision]]
		FROM {{users}} [[user]]
		JOIN {{leafen_account_profile_state}} [[profile]]
			ON [[profile]].[[user_id]]=[[user]].[[id]]
		WHERE [[user]].[[id]] IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY [[user]].[[id]]
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	apps, err := integrationAccountUsers(app, ordered)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		profiles = append(profiles, commentActorProfile{
			UserID: row.UserID, DisplayName: row.DisplayName,
			Avatar: row.Avatar, Revision: row.Revision, App: apps[row.UserID],
		})
	}
	return profiles, nil
}

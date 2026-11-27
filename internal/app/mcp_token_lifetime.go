package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent's key — made on the settings' or the developer page, traded for
// a one-time code, or made for an agent that asked from its computer — has
// a life, as Notion's personal access tokens: 7, 30, 90 or 180 days or a
// year, chosen as it is made, a year unless chosen (agent key decision of
// 2026-11-16). An organization of its workspaces that holds keys to a
// shorter life holds it to that. Before it ends its person hears of it in
// their inbox and its agent in its answers (mcp_token_expiry_notice.go);
// making its key again on its page gives the new key the same life from
// then. An OAuth connection's access token keeps its own short life,
// renewed by its refresh token while the agent goes on using it
// (mcp_oauth_tokens.go).

// mcpTokenDefaultLifetime is a key's life when none is chosen.
const mcpTokenDefaultLifetime = "1y"

var errMCPTokenLifetime = errors.New("unknown key lifetime")

// mcpTokenLifetimeDays are the days a one-time code keeps for the life its
// key will have (leafen_mcp_connection_codes.expires_in_days; 0 for the
// default).
var mcpTokenLifetimeDays = map[string]int{"7d": 7, "30d": 30, "90d": 90, "180d": 180, "1y": 365}

func mcpTokenLifetimeKnown(lifetime string) bool {
	_, known := personalAccessTokenLifetimes[lifetime]
	return lifetime == "" || known
}

// mcpTokenLifetimeOfDays is the life a code's days stand for.
func mcpTokenLifetimeOfDays(days int) string {
	for lifetime, count := range mcpTokenLifetimeDays {
		if count == days {
			return lifetime
		}
	}
	return ""
}

// mcpTokenExpiry is when a key made at created with this life ("" for the
// default) ends: its life, or sooner when an organization of its
// workspaces holds keys to fewer days.
func mcpTokenExpiry(app core.App, workspaceIDs []string, created int64, lifetime string) (int64, error) {
	if lifetime == "" {
		lifetime = mcpTokenDefaultLifetime
	}
	grow, known := personalAccessTokenLifetimes[lifetime]
	if !known {
		return 0, errMCPTokenLifetime
	}
	expires := grow(time.UnixMilli(created)).UnixMilli()
	days, err := organizationKeyMaxDays(app, workspaceIDs)
	if err != nil {
		return 0, err
	}
	if held := created + int64(days)*24*int64(time.Hour/time.Millisecond); days > 0 && held < expires {
		expires = held
	}
	return expires, nil
}

// mcpTokenRenewedExpiry is when a key made again now ends: the life the key
// it replaces had, from now, within its organizations' days.
func mcpTokenRenewedExpiry(app core.App, workspaceIDs []string, madeAt, expiresAt, now int64) (int64, error) {
	if expiresAt <= madeAt {
		return mcpTokenExpiry(app, workspaceIDs, now, "")
	}
	expires := now + (expiresAt - madeAt)
	days, err := organizationKeyMaxDays(app, workspaceIDs)
	if err != nil {
		return 0, err
	}
	if held := now + int64(days)*24*int64(time.Hour/time.Millisecond); days > 0 && held < expires {
		expires = held
	}
	return expires, nil
}

// organizationKeyMaxDays is the fewest days the organizations of these
// workspaces give a key, or 0 when none holds keys to a life.
func organizationKeyMaxDays(app core.App, workspaceIDs []string) (int, error) {
	shortest := 0
	for _, workspaceID := range workspaceIDs {
		var organizationID string
		err := app.DB().NewQuery(`SELECT [[organization]] FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": workspaceID}).Row(&organizationID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && organizationID == "" {
			continue
		}
		if err != nil {
			return 0, err
		}
		policy, err := loadOrganizationUserKeyPolicy(app, organizationID)
		if err != nil {
			return 0, err
		}
		if policy.MaxDays > 0 && (shortest == 0 || policy.MaxDays < shortest) {
			shortest = policy.MaxDays
		}
	}
	return shortest, nil
}

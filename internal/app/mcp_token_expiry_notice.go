package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Before an agent's key ends (mcp_token_lifetime.go) its person hears of it
// in their inbox — and so by mail and push as their settings say — and its
// agent in its answers, so the key is made again on its page, or the agent
// connected again, before work stops (agent key decision of 2026-11-16); an
// agent connection hears it makes its key again itself.
// Both come a week before its end; a key made for a week or less hears a day
// before. A key made again is told of again before its new end. An OAuth
// connection renews itself (mcp_oauth_tokens.go) and isn't told of.

const (
	mcpKeyEndNoticeWeek  = 7 * 24 * time.Hour
	mcpKeyEndNoticeDay   = 24 * time.Hour
	mcpKeyEndNoticeBatch = 200
	// An agent hears of its key's end at most this often.
	mcpKeyEndAgentEvery = 12 * time.Hour
	mcpKeyEndNoticePage = agentNoticePagePrefix + "key-ends"
)

var (
	mcpKeyEndNoticeTitle = mcpWords{"에이전트 연결 키가 곧 끝남: %s", "An agent’s key ends soon: %s"}
	mcpKeyEndNoticeLine  = mcpWords{
		"%s에 끝납니다. 계속 쓰려면 키 페이지에서 키를 새로 만들어 에이전트에 주거나 에이전트를 다시 연결하세요.",
		"It ends on %s. To keep using it, make its key again on its page and give the agent the new one, or connect the agent again.",
	}
	mcpKeyEndAgentLine = "This connection’s key ends on %s (%s). Its person makes its key again on its page, %s at Leafen’s " +
		"address (Developer tools › MCP tokens), and gives you the new one, or connects you again. Left to do before then: that to " +
		"your person. Until then, nothing changes."
	// An agent connection makes its key again itself, as its person's page
	// does (POST /mcp/connections/{id}/token; 2026-11-26: no way is their
	// screen's alone), the old key ending a week later.
	mcpKeyEndAgentSelfLine = "This connection’s key ends on %s (%s). Make it again yourself before then: leafen_call POST " + apiRoot +
		"/mcp/connections/%s/token with {\"revokeOld\": \"later\"} answers the new key (Leafen asks your person once in the " +
		"conversation), which goes where your client keeps this one; the old one goes on working for a week. Until then, nothing changes."
)

// mcpKeyEndNoticeWindow is how long before its end a key is told of, for a
// key that lives this long.
func mcpKeyEndNoticeWindow(life time.Duration) time.Duration {
	if life <= mcpKeyEndNoticeWeek+mcpKeyEndNoticeDay {
		return mcpKeyEndNoticeDay
	}
	return mcpKeyEndNoticeWeek
}

func registerMCPTokenExpiryNotices(event *core.ServeEvent) {
	app := event.App
	// Stopped with the server (server_background.go).
	event.App.Cron().MustAdd("leafenAgentKeyEndNotices", "17 * * * *", func() {
		serverBackgroundOf(app).run("agent key end notices", func(context.Context) {
			if err := noticeEndingMCPTokens(app, time.Now()); err != nil {
				app.Logger().Warn("leafen agent key end notices failed", "error", err)
			}
		})
	})
}

type endingMCPToken struct {
	ID         string `db:"id"`
	OwnerID    string `db:"owner_id"`
	ClientName string `db:"client_name"`
	ExpiresAt  int64  `db:"token_expires_at"`
}

// noticeEndingMCPTokens tells the person of each working hand-made key
// whose end has come within its window and that it wasn't told of.
func noticeEndingMCPTokens(app core.App, now time.Time) error {
	rows := []endingMCPToken{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[token_expires_at]] FROM {{leafen_mcp_connections}}
		WHERE [[oauth_client_id]] = '' AND [[app_install_id]] = '' AND [[revoked_at]] = 0
			AND [[token_expires_at]] > {:now} AND [[token_expires_at]] <= {:week}
			AND [[expiry_notice_for]] <> [[token_expires_at]]
			AND [[token_expires_at]] - {:dayMillis} <= {:now} + CASE
				WHEN [[token_expires_at]] - MAX([[created]], [[token_made_at]]) <= {:shortLife} THEN 0 ELSE {:weekMillis} - {:dayMillis} END
		ORDER BY [[token_expires_at]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"now": now.UnixMilli(), "week": now.Add(mcpKeyEndNoticeWeek).UnixMilli(),
		"dayMillis": mcpKeyEndNoticeDay.Milliseconds(), "weekMillis": mcpKeyEndNoticeWeek.Milliseconds(),
		"shortLife": (mcpKeyEndNoticeWeek + mcpKeyEndNoticeDay).Milliseconds(), "limit": mcpKeyEndNoticeBatch,
	}).All(&rows); err != nil {
		return err
	}
	for _, row := range rows {
		if err := noticeEndingMCPToken(app, row, now); err != nil {
			return err
		}
	}
	return nil
}

func noticeEndingMCPToken(app core.App, row endingMCPToken, now time.Time) error {
	workspaceID, err := personNoticeWorkspace(app, row.OwnerID, "")
	if err != nil {
		return err
	}
	if workspaceID != "" {
		language, err := serverTextLanguage(app, row.OwnerID, nil)
		if err != nil {
			language = accountLanguageEn
		}
		ends := mcpKeyEndDate(time.UnixMilli(row.ExpiresAt).In(accountLocation(app, row.OwnerID)), language)
		at := now.UnixMilli()
		stamp := fmt.Sprint(row.ExpiresAt)
		if _, err := upsertProductNotifications(context.Background(), app, []productNotificationProjection{{
			ID: appNoticeID("agent-key-ends", row.ID, stamp, row.OwnerID), EventID: appNoticeID("agent-key-ends-event", row.ID, stamp),
			WorkspaceID: workspaceID, UserID: row.OwnerID, ActivityKey: "agent-key-ends:" + row.ID + ":" + stamp, Kind: "system",
			PageID: mcpKeyEndNoticePage, Title: fmt.Sprintf(mcpKeyEndNoticeTitle.in(language), appOneLine(row.ClientName, 80)),
			Preview: fmt.Sprintf(mcpKeyEndNoticeLine.in(language), ends), Target: developerMcpTokenPath(row.ID),
			OccurredAt: at, Personal: true,
		}}, at); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_mcp_connections}} SET [[expiry_notice_for]] = [[token_expires_at]]
		WHERE [[id]] = {:id} AND [[token_expires_at]] = {:ends}`).Bind(dbx.Params{"id": row.ID, "ends": row.ExpiresAt}).Execute()
	return err
}

// developerMcpTokenPath is a token's own page (web lib/router.ts).
func developerMcpTokenPath(id string) string {
	return "/developers/mcp-tokens/" + id
}

func mcpKeyEndDate(day time.Time, language string) string {
	if language == accountLanguageKo {
		return day.Format("2006년 1월 2일")
	}
	return day.Format("January 2, 2006")
}

// mcpKeyEndInResponses adds to a tool's answer, at most twice a day, when the
// calling agent's own key ends within its window: the agent tells its person.
// It holds leafen_call's answer of a tool it names too (mcpNamedTools), so
// the note stands in leafen_call's answer, not in the tool's within it.
func mcpKeyEndInResponses(app core.App) mcp.Middleware {
	told := sync.Map{}
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			called, ok := result.(*mcp.CallToolResult)
			if err != nil || method != "tools/call" || !ok || called == nil {
				return result, err
			}
			authority, authorityErr := mcpAuthorityFromContext(ctx)
			if authorityErr != nil || authority.ClientID != mcpGovernanceManualClientID || authority.CredentialExpiresAt <= 0 {
				return result, err
			}
			now := time.Now()
			ends := time.UnixMilli(authority.CredentialExpiresAt)
			if ends.Sub(now) > mcpKeyEndNoticeWindow(ends.Sub(time.UnixMilli(authority.Created))) || !ends.After(now) {
				return result, err
			}
			key := authority.ConnectionID + "\x00" + fmt.Sprint(authority.CredentialExpiresAt)
			if last, seen := told.Load(key); seen && now.Sub(last.(time.Time)) < mcpKeyEndAgentEvery {
				return result, err
			}
			told.Store(key, now)
			left := ends.Sub(now).Round(time.Hour)
			notice := fmt.Sprintf(mcpKeyEndAgentLine, personClockText(ends, personLocation(app, authority.owner())), "in about "+mcpKeyEndLeft(left),
				developerMcpTokenPath(authority.ConnectionID))
			if authority.Agent {
				notice = fmt.Sprintf(mcpKeyEndAgentSelfLine, personClockText(ends, personLocation(app, authority.owner())),
					"in about "+mcpKeyEndLeft(left), authority.ConnectionID)
			}
			// Inside the answer, as Leafen's notes go (mcp_answer_notes.go).
			mcpAnswerNote(called, mcpAnswerKeyEndsField, notice, "Leafen: "+notice)
			return result, err
		}
	}
}

func mcpKeyEndLeft(left time.Duration) string {
	switch days, hours := int(left.Hours()/24), max(1, int(left.Hours())); {
	case days > 1:
		return fmt.Sprintf("%d days", days)
	case days == 1:
		return "a day"
	case hours > 1:
		return fmt.Sprintf("%d hours", hours)
	default:
		return "an hour"
	}
}

package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// "모두 맡기기(묻지 않음)" on an MCP token (decided 2026-11-13): on, its agent
// does the actions the key rules confirm without asking its person, as
// before Leafen asked (mcp_action_approvals.go) — kept as asks_irreversible
// 0, so a token made before keeps doing what it did. An administrator's
// safeguards over apps, agents, keys, sign-in and sharing still ask
// (user_key_admin_routes.go). Its person sets it on the token's page
// (mcp_token_settings.go); its agent sets its own token's here, in their
// conversation, never another token's: turning it on asks its person once,
// as person_ask.go asks, and turning it off goes ahead at once.

const (
	mcpHandsAllOverPath      = "/mcp/connections/current/hands-all-over"
	mcpHandsAllOverBodyBytes = 1024
)

type mcpHandsAllOverInput struct {
	HandsAllOver *bool `json:"handsAllOver"`
}

type mcpHandsAllOverView struct {
	HandsAllOver bool `json:"handsAllOver"`
}

var (
	mcpHandsAllOverAskEffect = mcpWords{
		"켜면 이 토큰의 에이전트가 영구 삭제, 공유·공개·권한 바꾸기, 멤버·게스트 바꾸기, 계정·백업·조직 관리를 묻지 않고 바로 합니다. " +
			"앱 감독 같은 관리자 안전장치는 그래도 묻습니다. 리펜의 토큰 화면이나 대화에서 언제든 다시 끌 수 있고, 끌 때는 묻지 않습니다.",
		"Turned on, this token’s agent deletes for good, changes sharing, publishing, permissions, members or guests, and manages accounts, " +
			"backups or the organization without asking. An administrator’s safeguards, such as approving every app action, still ask. " +
			"It can be turned off again anytime, on the token’s page in Leafen or in the conversation, without asking."}
)

func registerMCPHandsAllOverRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+mcpHandsAllOverPath, readOwnMCPTokenHandsAllOver)
	event.Router.PUT(apiRoot+mcpHandsAllOverPath, setOwnMCPTokenHandsAllOver).Bind(apis.BodyLimit(mcpHandsAllOverBodyBytes))
}

// mcpHandsAllOverTurnsOn says a request to the route turns the key's own
// token's "모두 맡기기" on, which waits for its person's yes, asked once its
// handler read the request; a body that isn't one it refuses before asking.
func mcpHandsAllOverTurnsOn(app core.App, key userKeyAuthority, body []byte) bool {
	input := mcpHandsAllOverInput{}
	if json.Unmarshal(body, &input) != nil {
		return true
	}
	if input.HandsAllOver == nil || !*input.HandsAllOver {
		return false
	}
	asks, err := mcpTokenAsksAction(app, key.ConnectionID)
	return err != nil || asks
}

// ownMCPTokenAsks is whether the request's own token asks its person, or
// answered says it was refused: no token sends it, or the token is gone.
func ownMCPTokenAsks(event *core.RequestEvent) (key userKeyAuthority, asks bool, answered bool, err error) {
	key, keyed := userKeyOf(event)
	if !keyed || key.AppInstallID != "" {
		return key, false, true, apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing,
			"No MCP token sends this: an agent sends it with its own token. Logged in, its person sets it on the token's page.")
	}
	stored := 1
	err = event.App.DB().NewQuery(`SELECT [[asks_irreversible]] FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:id} AND [[revoked_at]] = 0 AND [[app_install_id]] = ''`).
		Bind(dbx.Params{"id": key.ConnectionID}).Row(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return key, false, true, apiError(event, http.StatusNotFound, errorCodeMCPConnectionMissing, "MCP connection not found.")
	}
	if err != nil {
		return key, false, true, mcpTokenSettingsFailure(event, err)
	}
	return key, stored != 0, false, nil
}

func readOwnMCPTokenHandsAllOver(event *core.RequestEvent) error {
	_, asks, answered, err := ownMCPTokenAsks(event)
	if answered {
		return err
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, mcpHandsAllOverView{HandsAllOver: !asks})
}

// setOwnMCPTokenHandsAllOver turns it on, once its person said yes to this
// request (the key rules ask them: mcpHandsAllOverTurnsOn), or off.
func setOwnMCPTokenHandsAllOver(event *core.RequestEvent) error {
	key, _, answered, err := ownMCPTokenAsks(event)
	if answered {
		return err
	}
	body, err := io.ReadAll(io.LimitReader(event.Request.Body, mcpHandsAllOverBodyBytes+1))
	input := mcpHandsAllOverInput{}
	if err != nil || len(body) > mcpHandsAllOverBodyBytes || json.Unmarshal(body, &input) != nil || input.HandsAllOver == nil {
		return apiError(event, http.StatusBadRequest, errorCodeMCPTokenInvalid, `Send {"handsAllOver": true} or {"handsAllOver": false}.`)
	}
	// Turning it on asks once the request reads (user_key_acting_ask.go).
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if _, err := event.App.DB().Update(mcpConnectionsTable, dbx.Params{"asks_irreversible": !*input.HandsAllOver},
		dbx.HashExp{"id": key.ConnectionID}).Execute(); err != nil {
		return mcpTokenSettingsFailure(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, mcpHandsAllOverView{HandsAllOver: *input.HandsAllOver})
}

package app

import (
	"encoding/json"

	"github.com/pocketbase/pocketbase/core"
)

// What a key turns off that asks its person — a token's "모두 맡기기", an
// outside service's tool run without asking, or all of a service's tools
// always allowed — waits for their yes in the conversation, as turning
// "모두 맡기기" on for its own token does (mcp_token_hand_over.go, decided
// 2026-11-13); turning any back to asking, and every other setting, goes
// ahead at once. A body that isn't one asks, and its handler refuses it
// before asking.

// mcpTokenSettingsStopAsking says a key's change to a connection's page
// turns its "모두 맡기기" on.
func mcpTokenSettingsStopAsking(_ core.App, _ userKeyAuthority, body []byte) bool {
	input := struct {
		HandsAllOver *bool `json:"handsAllOver"`
	}{}
	return json.Unmarshal(body, &input) != nil || input.HandsAllOver != nil && *input.HandsAllOver
}

// mcpServiceToolStopsAsking says a key's change to a service's tool runs it
// without asking.
func mcpServiceToolStopsAsking(_ core.App, _ userKeyAuthority, body []byte) bool {
	input := struct {
		Run *string `json:"run"`
	}{}
	return json.Unmarshal(body, &input) != nil || input.Run != nil && *input.Run == mcpServiceRunAuto
}

// mcpServiceStopsAsking says a key's change to a service lets all its tools
// run without asking.
func mcpServiceStopsAsking(_ core.App, _ userKeyAuthority, body []byte) bool {
	input := struct {
		AlwaysAllow *bool `json:"alwaysAllow"`
	}{}
	return json.Unmarshal(body, &input) != nil || input.AlwaysAllow != nil && *input.AlwaysAllow
}

// mcpConnectionSettingsAskEffects say what a yes to each lets happen.
var mcpConnectionSettingsAskEffects = map[string]mcpWords{
	"PATCH /mcp/connections/{id}/settings": mcpHandsAllOverAskEffect,
	"PUT " + agentSignInSettingPath: {
		"켜면 내 에이전트가 리펜 로그인 화면의 코드를 승인해 브라우저를 내 계정으로 로그인시킬 수 있습니다. 계정 보안 화면이나 대화에서 언제든 다시 끌 수 있고, 끌 때는 묻지 않습니다.",
		"Turned on, your agents may approve the code on Leafen’s login screen, logging that browser in as you. It can be turned off again " +
			"anytime, on the account security screen or in the conversation, without asking."},
	"PATCH /mcp-services/tools/{serviceId}/{tool}": {
		"이 외부 서비스 도구를 에이전트가 쓸 때 더는 묻지 않습니다. 연결 화면이나 대화에서 다시 ‘항상 묻기’로 바꿀 수 있고, 그때는 묻지 않습니다.",
		"Agents use this external service’s tool without asking again. It can be set back to asking first, on the connections screen or " +
			"in the conversation, without asking."},
	"PATCH /mcp-services/{serviceId}": {
		"이 외부 서비스의 모든 도구를 에이전트가 묻지 않고 씁니다. 연결 화면이나 대화에서 다시 끌 수 있고, 끌 때는 묻지 않습니다.",
		"Agents use all of this external service’s tools without asking. It can be turned off again, on the connections screen or in " +
			"the conversation, without asking."},
}

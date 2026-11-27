package app

import (
	"fmt"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A service's tool that waits for the person asks them inside their
// conversation with the agent (person_ask.go), in their words: which tool
// of which service, what it may do, and its input. Notices about a service
// (its address changing, mcp_service_address_change.go) carry
// "service:<id>" where a page would be named and show while the person
// keeps the service.

const mcpServiceNoticePagePrefix = "service:"

// mcpServiceNoticeVisible reports whether a person still keeps the service
// a notice names.
func mcpServiceNoticeVisible(app core.App, userID, serviceID string) (bool, error) {
	kept := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_mcp_services}} WHERE [[id]] = {:id} AND [[user_id]] = {:user}`).
		Bind(dbx.Params{"id": serviceID, "user": userID}).Row(&kept)
	return kept > 0, err
}

var (
	mcpServiceAskRun         = mcpWords{"에이전트가 %s의 “%s” 도구를 쓰려고 합니다.", "An agent wants to use “%[2]s” of %[1]s."}
	mcpServiceAskDestructive = mcpWords{"이 도구는 그 서비스에서 무언가를 바꾸거나 삭제할 수 있습니다.", "It may change or delete something in that service."}
	mcpServiceAskAsked       = mcpWords{"이 도구는 실행하기 전에 묻도록 설정되어 있습니다.", "This tool is set to ask before it runs."}
	mcpServiceAskForHour     = mcpWords{"이 연결에서 이 도구를 1시간 동안 묻지 않고 씁니다.", "This connection uses this tool without asking for an hour."}
	mcpServiceAskAlways      = mcpWords{"%s의 모든 도구를 앞으로 묻지 않고 씁니다. 설정 → 연결의 “관리”에서 되돌릴 수 있습니다.",
		"All of %s’s tools run without asking from now on. Undo it under “Manage” in Settings → Connections."}
)

// mcpServiceAsk is the question for a service's tool, in the person's
// language.
func mcpServiceAsk(app core.App, authority mcpAuthority, service MCPService, tool MCPServiceTool, canonical string) personAsk {
	language, err := serverTextLanguage(app, authority.owner(), nil)
	if err != nil {
		language = accountLanguageEn
	}
	name := appOneLine(service.Name, 80)
	effect := mcpServiceAskAsked
	if tool.Destructive {
		effect = mcpServiceAskDestructive
	}
	return personAsk{Question: fmt.Sprintf(mcpServiceAskRun.in(language), name, firstNonempty(tool.Title, tool.Name)),
		Effect: effect.in(language), Input: canonical, ForHour: mcpServiceAskForHour.in(language),
		Always: fmt.Sprintf(mcpServiceAskAlways.in(language), name), Language: language}
}

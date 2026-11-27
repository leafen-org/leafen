package app

// The words an agent's action waiting for its person reads in
// (mcp_action_approval_words.go), Korean then English: each confirmed
// route's action, the actions a route's body picks among, the fields a body
// sets and the values those fields take. A route or field none of these
// name reads as its kind of action, or as the field's name as sent.

type mcpWords [2]string

func (words mcpWords) in(language string) string {
	if language == accountLanguageKo {
		return words[0]
	}
	return words[1]
}

var mcpActionNames = map[string]mcpWords{
	// The server's administration.
	"PUT /admin/accounts/{accountId}/administrator":                          {"서버 관리자 지정 바꾸기", "Change who administers the server"},
	"POST /admin/accounts/{accountId}/cleanup":                               {"계정 정리 시작하기", "Start cleaning up an account"},
	"POST /admin/accounts/{accountId}/cleanup/{jobId}/commit":                {"계정 정리 마치기", "Finish cleaning up an account"},
	"POST /admin/accounts/{accountId}/sessions/revoke":                       {"계정의 모든 세션 끝내기", "End all of an account’s sessions"},
	"POST /admin/accounts/{accountId}/sessions/{sessionId}/revoke":           {"계정의 세션 끝내기", "End an account’s session"},
	"PATCH /admin/accounts/{accountId}/state":                                {"계정 상태 바꾸기", "Change an account’s state"},
	"POST /admin/backups/schedule/restore":                                   {"백업에서 복원 예약하기", "Schedule a restore from a backup"},
	"DELETE /admin/backups/schedule/restore":                                 {"예약한 복원 취소하기", "Cancel a scheduled restore"},
	"POST /admin/backups/schedule/restore/undo":                              {"복원 되돌리기", "Undo a restore"},
	"POST /admin/backups/replica/restore":                                    {"복제본으로 서버 데이터 바꾸기", "Replace the server’s data from a replica"},
	"PUT /admin/backups/standby/join":                                        {"대기 서버로 합류하기", "Join as a standby server"},
	"POST /admin/backups/standby/takeover":                                   {"대기 서버로 전환하기", "Take over from the main server"},
	"POST /admin/product-exports":                                            {"서버 전체 내보내기", "Export the whole server"},
	"POST /admin/server-move/out":                                            {"서버를 다른 곳으로 옮기기", "Move the server elsewhere"},
	"POST /admin/server-move/in":                                             {"다른 서버를 이 서버로 옮겨 받기", "Receive another server here"},
	"DELETE /admin/apps/installs/{installId}/credentials":                    {"사람들이 앱에 준 계정 정보 지우기", "Delete the credentials people gave an app"},
	"DELETE /admin/apps/installs/{installId}/secrets/{name}":                 {"앱 비밀값 지우기", "Delete an app’s secret"},
	"DELETE /admin/backups/protection/{way}":                                 {"백업 보호 수단 없애기", "Remove a way backups are protected"},
	"POST /admin/workspaces/{workspaceId}/files/{fileId}/delete":             {"파일 영구 삭제", "Delete a file for good"},
	"POST /admin/workspaces/{workspaceId}/pages/{pageId}/delete-permanently": {"페이지 영구 삭제", "Delete a page for good"},
	"POST /admin/workspaces/{workspaceId}/pages/{pageId}/take-over":          {"비공개 페이지를 워크스페이스 소유자에게 열기", "Open a private page to the workspace’s owner"},
	"PUT /admin/apps/oversight":                                              {"앱의 모든 동작 승인 바꾸기", "Change whether a person approves every app action"},
	"PUT /admin/apps/allowlist":                                              {"사람이 직접 추가할 수 있는 앱 바꾸기", "Change which apps people may add themselves"},
	"PUT /admin/apps/settings":                                               {"앱 정책 바꾸기", "Change the app policies"},
	"PUT /admin/apps/strict":                                                 {"앱 연결은 이 서버를 거쳐서만 바꾸기", "Change whether apps connect only through this server"},
	"PATCH /admin/apps/installs/{installId}/status":                          {"앱 실행 상태 바꾸기", "Change whether an app runs"},
	"PUT /admin/apps/installs/{installId}/grants":                            {"앱을 사용할 수 있는 사람 바꾸기", "Change who can use an app"},
	"PUT /admin/apps/installs/{installId}/guests":                            {"앱 게스트 링크 만들기 허용 바꾸기", "Change whether an app’s guest links may be made"},
	"PUT /admin/apps/installs/{installId}/endpoint-public":                   {"앱 주소를 누구에게나 열기 바꾸기", "Change whether an app’s address is open to anyone"},
	"PUT /admin/apps/installs/{installId}/credentials":                       {"사람들이 앱에 자기 계정 정보를 줄 수 있게 하기 바꾸기", "Change whether people may give an app their credentials"},
	"PATCH /admin/settings":                                                  {"서버 설정 바꾸기", "Change the server’s settings"},

	// Keys, connections and groups people hold.
	"POST /workspaces/{workspaceId}/personal-access-tokens/{tokenId}/revoke":    {"개인 액세스 토큰 철회", "Revoke a personal access token"},
	"POST /organizations/{organizationId}/notion-admin-tokens/{tokenId}/revoke": {"조직 관리 API 토큰 철회", "Revoke an organization’s Admin API token"},
	"DELETE /workspaces/{workspaceId}/groups/{groupId}":                         {"그룹 삭제", "Delete a group"},
	"DELETE /workspaces/{workspaceId}/internal-connections/{connectionId}":      {"내부 연결 삭제", "Delete an internal connection"},

	// The agent's own token.
	"PUT " + mcpHandsAllOverPath: {"이 MCP 토큰의 ‘모두 맡기기(묻지 않음)’ 켜기", "Turn on “Hand it all over (don’t ask)” for this MCP token"},
	// The person's own sign-in.
	"DELETE /account/mfa/totp":         {"2단계 인증 끄기", "Turn off 2-step verification"},
	"POST /account/mfa/recovery-codes": {"복구 코드 재생성", "Regenerate recovery codes"},
	"DELETE /account/emails":           {"이메일 주소 제거", "Remove an email address"},
	"POST /account/deletion":           {"내 계정 삭제", "Delete my account"},
	"POST /account/password":           {"비밀번호 정하기", "Set a password"},
	"PUT /account/password":            {"비밀번호 바꾸기", "Change the password"},
	"DELETE /account/password":         {"비밀번호 없애기", "Remove the password"},
	// Pages, databases and their sharing.
	"PUT /data-sources/{dataSourceId}/properties/{propertyId}/access":          {"속성 사용 권한 바꾸기", "Change a property’s access"},
	"POST /database-rows/{rowId}/page/delete-permanently":                      {"페이지 영구 삭제", "Delete a page for good"},
	"POST /database-rows/{rowId}/page/permissions":                             {"페이지 공유 권한 바꾸기", "Change who the page is shared with"},
	"POST /pages/{pageId}/database-properties/{propertyId}/delete-permanently": {"속성 영구 삭제", "Delete a property for good"},
	"POST /pages/{pageId}/permanent-delete":                                    {"페이지 영구 삭제", "Delete a page for good"},
	"POST /pages/{pageId}/public-mutations":                                    {"페이지 공유·게시 바꾸기", "Change the page’s sharing or publishing"},
	"POST /workspaces/{workspaceId}/page-guest-links":                          {"게스트 초대 링크 만들기", "Make a guest invite link"},
	"POST /pages/{pageId}/share-inheritance":                                   {"상위 페이지 권한 상속 바꾸기", "Change what the page inherits from its parent"},
	"POST /pages/{pageId}/leave":                                               {"공유받은 페이지에서 나가기", "Leave a page shared with you"},
	"POST /pages/{pageId}/app-connections":                                     {"페이지의 앱 연결 바꾸기", "Change the page’s app connections"},
	"POST /pages/{pageId}/agent-grant-scope":                                   {"에이전트가 닿는 범위 바꾸기", "Change what agents reach from the page"},
	"POST /pages/{pageId}/page-rules":                                          {"페이지 액세스 규칙 바꾸기", "Change the page’s access rules"},
	"POST /access-requests/{requestId}/approve":                                {"액세스 요청 승인하기", "Approve an access request"},
	// Workspaces and their people.
	"DELETE /workspaces/{workspaceId}":                                  {"워크스페이스 삭제", "Delete the workspace"},
	"POST /workspaces/{workspaceId}/file-cleanup":                       {"쓰지 않는 파일 영구 삭제", "Delete unused files for good"},
	"POST /workspaces/{workspaceId}/guests/remove":                      {"게스트 내보내기", "Remove guests"},
	"DELETE /workspaces/{workspaceId}/invitations/{invitationId}":       {"초대 취소하기", "Cancel an invitation"},
	"POST /workspaces/{workspaceId}/members":                            {"멤버 추가하기", "Add a member"},
	"DELETE /workspaces/{workspaceId}/members/{userId}":                 {"멤버 내보내기", "Remove a member"},
	"PUT /workspaces/{workspaceId}/members/{userId}":                    {"멤버 역할 바꾸기", "Change a member’s role"},
	"PUT /workspaces/{workspaceId}/members/{userId}/restriction":        {"멤버 제한 바꾸기", "Change a member’s restriction"},
	"POST /workspaces/{workspaceId}/owner-transfer":                     {"워크스페이스 소유권 넘기기", "Hand the workspace to a new owner"},
	"POST /workspaces/{workspaceId}/trash/empty":                        {"휴지통 비우기", "Empty the trash"},
	"POST /workspaces/{workspaceId}/deleted-pages/{pageId}/purge":       {"영구 삭제한 페이지 바로 지우기", "Erase a page deleted for good right away"},
	"POST /workspaces/{workspaceId}/deleted-databases/{blockId}/delete": {"휴지통의 데이터베이스 영구 삭제", "Delete a database in the trash for good"},
	"POST /workspaces/{workspaceId}/move-out":                           {"워크스페이스를 다른 서버로 보내기", "Move the workspace to another server"},
	// An organization's administration.
	"PUT /organizations/{organizationId}/sign-in-policy":         {"조직 로그인 정책 바꾸기", "Change the organization’s login policy"},
	"PUT /organizations/{organizationId}/guest-mfa":              {"게스트 2단계 인증 요구 바꾸기", "Change whether guests need 2-step verification"},
	"PUT /organizations/{organizationId}/account-history-policy": {"계정 기록 정책 바꾸기", "Change the account history policy"},
	"PUT /organizations/{organizationId}/user-key-policy":        {"조직의 키 정책 바꾸기", "Change the organization’s key policy"},
	"PUT /organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}": {"허용하는 MCP 클라이언트 정책 바꾸기",
		"Change which MCP clients are allowed"},
	"POST /organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}/clients": {"MCP 클라이언트 허용하기", "Allow an MCP client"},
	"DELETE /organizations/{organizationId}/mcp-governance/workspaces/{workspaceId}/clients/{clientId}": {"허용한 MCP 클라이언트 빼기",
		"Remove an allowed MCP client"},
	"PUT /organizations/{organizationId}/sharing-security-policy":                       {"조직 공유·보안 정책 바꾸기", "Change the organization’s sharing and security policy"},
	"PATCH /organizations/{organizationId}/managed-user-policies":                       {"관리 대상 사용자 정책 바꾸기", "Change the managed users’ policies"},
	"PUT /organizations/{organizationId}/audit-retention":                               {"감사 로그 보관 기간 바꾸기", "Change how long the audit log is kept"},
	"POST /organizations/{organizationId}/discovery-exports":                            {"조직 콘텐츠 내보내기(eDiscovery)", "Export the organization’s content for discovery"},
	"PUT /organizations/{organizationId}/dlp":                                           {"데이터 유출 방지 설정 바꾸기", "Change data loss prevention"},
	"POST /organizations/{organizationId}/legal-holds/{legalHoldId}/release":            {"법적 보존 풀기", "Release a legal hold"},
	"POST /organizations/{organizationId}/managed-users/logout":                         {"관리 대상 사용자 모두 로그아웃", "Log managed users out"},
	"POST /organizations/{organizationId}/managed-users/password-reset":                 {"관리 대상 사용자 비밀번호 재설정", "Reset managed users’ passwords"},
	"PATCH /organizations/{organizationId}/managed-users/{userId}":                      {"관리 대상 사용자 바꾸기", "Change a managed user"},
	"POST /organizations/{organizationId}/managed-users/{userId}/deletion":              {"관리 대상 사용자 삭제 예약하기", "Schedule a managed user’s deletion"},
	"DELETE /organizations/{organizationId}/managed-users/{userId}/deletion":            {"관리 대상 사용자 삭제 예약 취소하기", "Cancel a managed user’s scheduled deletion"},
	"POST /organizations/{organizationId}/managed-users/{userId}/emails":                {"관리 대상 사용자 이메일 추가하기", "Add a managed user’s email"},
	"PUT /organizations/{organizationId}/managed-users/{userId}/emails":                 {"관리 대상 사용자 이메일 바꾸기", "Change a managed user’s email"},
	"DELETE /organizations/{organizationId}/managed-users/{userId}/emails":              {"관리 대상 사용자 이메일 빼기", "Remove a managed user’s email"},
	"DELETE /organizations/{organizationId}/managed-users/{userId}/external-workspaces": {"관리 대상 사용자를 외부 워크스페이스에서 내보내기", "Remove a managed user from outside workspaces"},
	"POST /organizations/{organizationId}/managed-users/{userId}/logout":                {"관리 대상 사용자 로그아웃", "Log a managed user out"},
	"POST /organizations/{organizationId}/managed-users/{userId}/password-reset":        {"관리 대상 사용자 비밀번호 재설정", "Reset a managed user’s password"},
	"PUT /organizations/{organizationId}/managed-users/{userId}/suspension":             {"관리 대상 사용자 정지하기", "Suspend a managed user"},
	"DELETE /organizations/{organizationId}/managed-users/{userId}/suspension":          {"관리 대상 사용자 정지 풀기", "Lift a managed user’s suspension"},
	"PUT /organizations/{organizationId}/saml":                                          {"SAML 로그인 설정 바꾸기", "Change SAML login"},
	"PUT /organizations/{organizationId}/scim":                                          {"SCIM 설정 바꾸기", "Change SCIM provisioning"},
	"POST /organizations/{organizationId}/settings-mutations":                           {"조직 설정 바꾸기", "Change the organization’s settings"},
	"PUT /organizations/{organizationId}/storage-policy":                                {"저장 공간 정책 바꾸기", "Change the storage policy"},
}

// mcpActionSelfNames are what a route does where the person it acts on, its
// path's {userId}, is the person asking: a member removing themself leaves
// the workspace, as the workspace settings' "워크스페이스 나가기" does, the
// one way out of it (2026-11-23: leafen_operations found nothing for
// "워크스페이스 나가기" or "leave workspace", the route named only for
// removing someone else).
var mcpActionSelfNames = map[string]mcpWords{
	"DELETE /workspaces/{workspaceId}/members/{userId}": {"워크스페이스 나가기", "Leave the workspace"},
}

// mcpActionBodyNames are the actions a route's body picks with its
// "action" field, by route.
var mcpActionBodyNames = map[string]map[string]mcpWords{
	"POST /pages/{pageId}/public-mutations": {
		"set_web_sharing": {"웹 게시 켜고 끄기", "Turn publishing to the web on or off"}, "rotate_share_token": {"공유 링크 새로 만들기", "Make a new share link"},
		"publish_site": {"사이트 게시하기", "Publish a site"}, "unpublish_site": {"사이트 게시 그만두기", "Unpublish a site"},
		"grant_permission": {"페이지 공유하기", "Share the page"}, "update_permission": {"공유 권한 바꾸기", "Change someone’s access"},
		"remove_permission": {"공유에서 빼기", "Remove someone’s access"},
	},
	"POST /organizations/{organizationId}/settings-mutations": {
		"group_delete": {"조직 그룹 삭제", "Delete a group"}, "domain_remove": {"인증된 도메인 빼기", "Remove a verified domain"},
		"member_remove": {"조직 멤버 내보내기", "Remove a member from the organization"}, "owner_transfer": {"조직 소유권 넘기기", "Hand the organization to a new owner"},
		"feed_retention": {"피드 보관 기간 바꾸기", "Change how long the feed is kept"}, "analytics_retention": {"분석 보관 기간 바꾸기", "Change how long analytics are kept"},
	},
}

// mcpActionKindNames name what a route's path names, by its path value.
var mcpActionKindNames = map[string]mcpWords{
	"workspace": {"워크스페이스", "Workspace"}, "page": {"페이지", "Page"}, "database": {"데이터베이스", "Database"},
	"property": {"속성", "Property"}, "person": {"사람", "Person"}, "organization": {"조직", "Organization"},
	"group": {"그룹", "Group"}, "invitation": {"초대", "Invitation"}, "file": {"파일", "File"}, "legalHold": {"법적 보존", "Legal hold"},
	"app": {"앱", "App"},
}

var mcpActionFieldNames = map[string]mcpWords{
	"action": {"작업", "Action"}, "role": {"권한", "Access"}, "defaultLevel": {"기본 사용 권한", "Default access"},
	"exceptions": {"예외", "Exceptions"}, "level": {"사용 권한", "Access"}, "principalType": {"대상 종류", "Kind"},
	"principalId": {"대상", "Who"}, "userId": {"사람", "Person"}, "userIds": {"사람", "People"}, "email": {"이메일", "Email"},
	"emails": {"이메일", "Emails"}, "enabled": {"켜기", "On"}, "expiresAt": {"끝나는 때", "Ends"}, "scope": {"범위", "Scope"},
	"dataSourceId": {"데이터베이스", "Database"}, "propertyId": {"속성", "Property"}, "pageId": {"페이지", "Page"},
	"pageIds": {"페이지", "Pages"}, "navigationPageIds": {"탐색 페이지", "Navigation pages"}, "workspaceIds": {"워크스페이스", "Workspaces"},
	"groupId": {"그룹", "Group"}, "slug": {"주소", "Address"}, "title": {"제목", "Title"}, "administrator": {"서버 관리자", "Administrator"},
	"state": {"상태", "State"}, "restricted": {"제한", "Restricted"}, "inviteMessage": {"초대 메시지", "Invitation message"},
	"tenancyKey":   {"조직", "Organization"},
	"overseen":     {"앱의 모든 동작을 사람이 승인", "Approve every app action"},
	"handsAllOver": {"모두 맡기기(묻지 않음)", "Hand it all over (don’t ask)"},
	// An administrator's safeguards, in their screens' words (a route's own
	// field by its alias, below).
	"strict": {"앱 연결은 이 서버를 거쳐서만", "Apps connect only through this server"}, "strictNetwork": {"앱 연결은 이 서버를 거쳐서만", "Apps connect only through this server"},
	"installPolicy": {"앱 설치", "Who installs apps"}, "homeScreenAppsOff": {"홈 화면 앱 끄기", "Home screen apps off"},
	"unsignedAppsOff": {"서명 없는 앱 파일 끄기", "Unsigned app files off"}, "maxPackageBytes": {"앱 파일 최대 크기", "Largest app file"},
	"allowedApps": {"허용한 앱", "Allowed apps"}, "appStatus": {"실행 상태", "Whether it runs"}, "appGrants": {"사용할 수 있는 사람", "Who can use it"},
	"appEndpoint": {"앱 주소", "App address"}, "endpointOpen": {"누구에게나 열기", "Open to anyone"},
	"workspaceId": {"워크스페이스", "Workspace"}, "guestTwoStep": {"게스트 2단계 인증 요구", "Guests need 2-step verification"},
	"passwordTwoStep": {"비밀번호 로그인에 2단계 인증 요구", "Password login needs 2-step verification"},
	"keysAllowed":     {"멤버가 키를 만들 수 있음", "Members can create keys"}, "maxScope": {"최대 범위", "Widest scope"},
	"maxDays": {"키 만료(일)", "Keys expire after (days)"}, "retentionDays": {"보관 기간(일)", "Kept for (days)"},
	"publicWebSharing":     {"공개 폼·페이지 링크·사이트 허용", "Allow public forms, page links and sites"},
	"externalEmailSharing": {"외부 이메일과 공유 허용", "Allow sharing with external emails"}, "guestAccess": {"게스트 허용", "Allow guests"},
	"exports": {"페이지 내보내기 허용", "Allow page exports"}, "crossWorkspaceCopies": {"다른 워크스페이스로 페이지 복제 허용", "Allow duplicating pages to other workspaces"},
	// The person's own sign-in, and what proves it is them.
	"password": {"비밀번호", "Password"}, "code": {"인증 코드", "Authentication code"},
	"deletionEmail": {"확인으로 입력한 이메일", "Email typed to confirm"},
	// The server's settings (PATCH /admin/settings), by their paths in its body.
	"signupPolicy": {"가입할 수 있는 사람", "Who can sign up"}, "signupContact": {"가입 문의처", "Who to contact about signing up"},
	"memberAddPolicy": {"워크스페이스 멤버 추가", "Adding workspace members"}, "serverName": {"서버 이름", "Server name"},
	"smtp.enabled": {"SMTP 사용", "Use SMTP"}, "smtp.host": {"SMTP 호스트", "SMTP host"}, "smtp.port": {"SMTP 포트", "SMTP port"},
	"smtp.username": {"SMTP 사용자 이름", "SMTP username"}, "smtp.password": {"SMTP 비밀번호", "SMTP password"},
	"smtp.clearPassword": {"저장된 SMTP 비밀번호 지우기", "Clear saved SMTP password"}, "smtp.tlsMode": {"SMTP 암호화", "SMTP encryption"},
	"smtp.senderName": {"보내는 이름", "Sender name"}, "smtp.senderAddress": {"보내는 주소", "Sender address"},
	"smtp.requireEmailVerification": {"가입 이메일 인증 필요", "Require email verification for signup"},
	"providers.google.enabled":      {"Google 로그인 사용", "Use Google sign-in"}, "providers.google.clientId": {"Google 클라이언트 ID", "Google client ID"},
	"providers.google.clientSecret": {"Google 클라이언트 비밀", "Google client secret"},
	"providers.google.clearSecret":  {"저장된 Google 비밀 지우기", "Clear saved Google secret"},
	"providers.oidc.enabled":        {"OIDC 로그인 사용", "Use OIDC login"}, "providers.oidc.name": {"OIDC 표시 이름", "OIDC display name"},
	"providers.oidc.issuer": {"OIDC Issuer", "OIDC issuer"}, "providers.oidc.clientId": {"OIDC 클라이언트 ID", "OIDC client ID"},
	"providers.oidc.clientSecret": {"OIDC 클라이언트 비밀", "OIDC client secret"},
	"providers.oidc.clearSecret":  {"저장된 OIDC 비밀 지우기", "Clear saved OIDC secret"}, "providers.oidc.scopes": {"OIDC Scopes", "OIDC scopes"},
	"map.enabled": {"외부 지도 타일 사용", "Use external map tiles"}, "map.tileUrl": {"지도 타일 주소", "Map tile address"},
	"map.attribution": {"지도 출처 표시 문구", "Map attribution text"}, "map.attributionUrl": {"지도 출처 링크", "Map attribution link"},
	"placeSearch.enabled": {"장소 검색 사용", "Enable place search"}, "placeSearch.provider": {"장소 검색 공급자 형식", "Place search provider format"},
	"placeSearch.endpoint": {"장소 검색 주소", "Place search endpoint"}, "placeSearch.apiKey": {"장소 검색 API 키", "Place search API key"},
	"placeSearch.apiKeyId":    {"장소 검색 API 키 ID", "Place search API key ID"},
	"placeSearch.clearConfig": {"저장된 장소 검색 공급자 설정 지우기", "Remove saved place search provider settings"},
	"updates.enabled":         {"매일 새 버전 확인", "Check for new versions daily"},
	"switcherLists.enabled":   {"다른 리펜 서버에서 목록 보기", "Lists for other Leafen servers"}, "readLog.enabled": {"열람을 기록", "Record reads"},
}

// mcpActionFieldAliases name a route's own field apart from the same name
// elsewhere, for its words above and below.
var mcpActionFieldAliases = map[string]map[string]string{
	"PUT /admin/apps/allowlist":                            {"apps": "allowedApps"},
	"PATCH /admin/apps/installs/{installId}/status":        {"status": "appStatus"},
	"PUT /admin/apps/installs/{installId}/grants":          {"grants": "appGrants"},
	"PUT /admin/apps/installs/{installId}/endpoint-public": {"key": "appEndpoint", "open": "endpointOpen"},
	"PUT /organizations/{organizationId}/guest-mfa":        {"required": "guestTwoStep"},
	"POST /account/deletion":                               {"email": "deletionEmail"},
}

// mcpActionValueNames word a field's values, by field.
var mcpActionValueNames = func() map[string]map[string]mcpWords {
	levels := map[string]mcpWords{
		propertyAccessInherit: {"데이터베이스에서 상속", "Inherit from database"}, propertyAccessEdit: {"속성과 속성값 편집 허용", "Can edit property and values"},
		propertyAccessEditValues: {"값만 편집 허용", "Can edit values only"}, propertyAccessView: {"속성과 속성값 보기 허용", "Can view property and values"},
		propertyAccessViewProperty: {"속성만 보기 허용", "Can view property only"}, propertyAccessNone: {"사용 권한 없음", "No access"},
	}
	return map[string]map[string]mcpWords{
		"defaultLevel": levels, "level": levels,
		"role": {
			publicMutationRoleFullAccess: {"전체 허용", "Full access"}, publicMutationRoleEdit: {"편집 허용", "Can edit"},
			publicMutationRoleComment: {"댓글 허용", "Can comment"}, publicMutationRoleView: {"읽기 허용", "Can view"},
			workspaceRoleOwner: {"워크스페이스 소유자", "Workspace owner"}, workspaceRoleMember: {"멤버", "Member"}, workspaceRoleGuest: {"게스트", "Guest"},
		},
		"appStatus": {"active": {"켜짐", "On"}, "stopped": {"멈춤", "Stopped"}, "emergency": {"긴급 정지", "Emergency stop"}},
		"signupPolicy": {administratorSignupPolicyClosed: {"초대받은 사람만", "Invited people only"},
			administratorSignupPolicyPublic: {"누구나", "Anyone"}},
		"memberAddPolicy": {administratorMemberAddPolicyEnabled: {"켜짐", "On"}, administratorMemberAddPolicyDisabled: {"꺼짐", "Off"}},
		"smtp.tlsMode": {administratorSMTPTLSNone: {"없음", "None"}, administratorSMTPTLSStartTLS: {"STARTTLS", "STARTTLS"},
			administratorSMTPTLSTLS: {"TLS", "TLS"}},
		"installPolicy": {"admins": {"관리자만 설치", "Only administrators"}, "request": {"사람이 요청하면 관리자가 설치", "People ask, administrators install"},
			"allowlist": {"허용한 앱은 사람이 직접 추가", "People add allowed apps"}},
		"principalType": {
			publicMutationPrincipalUser: {"사람", "Person"}, publicMutationPrincipalEmail: {"이메일", "Email"},
			publicMutationPrincipalGroup: {"그룹", "Group"}, publicMutationPrincipalIntegration: {"연결", "Connection"},
			publicMutationPrincipalWorkspace: {"워크스페이스 전체", "Everyone in the workspace"},
		},
	}
}()

var (
	mcpWordsYes      = mcpWords{"예", "Yes"}
	mcpWordsNo       = mcpWords{"아니요", "No"}
	mcpWordsNothing  = mcpWords{"없음", "None"}
	mcpWordsMore     = mcpWords{"외 %d개", "and %d more"}
	mcpWordsCount    = mcpWords{"%d개", "%d"}
	mcpWordsUntitled = mcpWords{"제목 없음", "Untitled"}
	mcpWordsHidden   = mcpWords{"(가려 둠)", "(hidden)"}
	mcpWordsOn       = mcpWords{"켜짐", "On"}
	mcpWordsOff      = mcpWords{"꺼짐", "Off"}
)

// What loosening the person's own sign-in or deleting their account does
// (user_key_account_routes.go), as their account settings say it, and why
// it is asked every time.
var mcpActionOwnAccountEffects = func() map[string]mcpWords {
	asked := mcpWords{"본인 계정의 로그인 보안을 푸는 일이라 에이전트가 할 때는 ‘모두 맡기기’를 켠 토큰이어도 언제나 사람에게 묻습니다.",
		"This loosens your own login security, so an agent doing it always asks you, even with “Hand it all over” on."}
	effects := map[string]mcpWords{
		"DELETE /account/mfa/totp": {"비밀번호로 로그인한 뒤 인증 앱 코드가 더는 필요하지 않습니다.",
			"Logging in with the password no longer needs an authenticator app code."},
		"POST /account/mfa/recovery-codes": {"지금 저장해 둔 복구 코드는 더는 쓸 수 없고, 새 코드는 에이전트가 받습니다.",
			"The recovery codes saved now stop working, and the agent gets the new ones."},
		"DELETE /account/emails": {"그 주소로는 더 이상 이 계정에 로그인할 수 없습니다.", "You won’t be able to log in with that address anymore."},
	}
	for operation, effect := range effects {
		effects[operation] = mcpWords{effect[0] + " " + asked[0], effect[1] + " " + asked[1]}
	}
	// The account settings' warning (workspaceSettingsDialog
	// accountDeletionWarning), in the same words, with the workspaces it
	// lists above.
	effects["POST /account/deletion"] = mcpWords{
		"되돌릴 수 없습니다. 계정이 삭제되고, 다른 사람과 함께 쓰던 워크스페이스에서 모두 빠집니다. " +
			"내가 소유한 위 워크스페이스도 함께 삭제되어, 그 안의 다른 사람도 모두 액세스를 잃습니다. " +
			"본인 계정을 지우는 일이라 에이전트가 할 때는 ‘모두 맡기기’를 켠 토큰이어도 언제나 사람에게 묻습니다.",
		"This can’t be reversed. Your account is deleted, and you leave every workspace you share with others. " +
			"The workspaces above that you own are deleted too, and everyone else in them loses access. " +
			"This deletes your own account, so an agent doing it always asks you, even with “Hand it all over” on.",
	}
	return effects
}()

// The person's own sign-in as it is now, for what loosening it changes.
var (
	mcpWordsTwoStep          = mcpWords{"2단계 인증", "2-step verification"}
	mcpWordsRecoveryCodes    = mcpWords{"쓸 수 있는 복구 코드", "Usable recovery codes"}
	mcpWordsNewRecoveryCodes = mcpWords{"새로 만든 코드만(에이전트가 받습니다)", "Only the new ones (the agent gets them)"}
	mcpWordsSignInEmails     = mcpWords{"로그인할 수 있는 이메일", "Emails that log in"}
	// What deleting the account deletes and leaves, as its settings list them.
	mcpWordsDeletedWorkspaces = mcpWords{"영구 삭제되는 내 워크스페이스", "Workspaces you own, deleted for good"}
	mcpWordsLeftWorkspaces    = mcpWords{"계정이 제거되는 공유 워크스페이스", "Shared workspaces you’re removed from"}
	mcpWordsMembers           = mcpWords{"%s(멤버 %d명)", "%s (%d members)"}
)

// mcpWordsOutsideOrganizations names the workspaces outside any organization,
// as the apps' settings do.
var mcpWordsOutsideOrganizations = mcpWords{"조직 밖 워크스페이스", "Workspaces outside an organization"}

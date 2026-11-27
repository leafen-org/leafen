package app

// What an operation does, in Korean and English, where its name doesn't say
// it: a route named for the record it sets (presentation, metadata, a
// workspace's settings), the verb its contract uses (apply, csvMerge), or
// what it makes without whose (createAdministratorManagedAccount, an account
// for a person). The operations list gives it beside the name (does), and it
// is an operation's summary where the groups data gives none
// (nativeOperationSummary).
var nativeOperationDoesNames = map[string]mcpWords{
	"PATCH /pages/{id}/metadata":                   {"페이지 제목·아이콘·커버·전체 너비·잠금 바꾸기", "Change a page’s title, icon, cover, font, full width or lock"},
	"PUT /database-rows/{rowId}/page/presentation": {"행 페이지의 아이콘·커버·전체 너비 바꾸기", "Change a row page’s icon, cover, font or full width"},
	"PATCH /workspaces/{workspaceId}":              {"워크스페이스 이름·아이콘·도메인·처음 여는 페이지 바꾸기", "Change a workspace’s name, icon, domain or the page it opens on"},
	// 2026-11-24: "rename member" put a teamspace's change first, an
	// organization's managed user's change, which renames one, beside it.
	"PATCH /organizations/{organizationId}/managed-users/{userId}": {"관리 대상 사용자의 이름·이메일 바꾸기", "Change a managed user’s name or email"},
	"POST /workspaces/{workspaceId}/data-sources/{dataSourceId}/imports/csv": {"CSV와 병합: CSV 파일의 행을 데이터베이스로 가져오기",
		"Merge a CSV file into a database, importing its rows"},
	"POST /pages/{pageId}/database-starter": {"페이지에 바로 쓸 수 있는 데이터베이스 만들기", "Make a ready-made database on a page"},
	"POST /admin/accounts":                  {"사람의 계정 만들기", "Make an account for a person"},
	"GET /account/auth-audit":               {"내 로그인 기록 읽기: 이 계정에 로그인한 시도", "Read your login history: each attempt to log in to this account"},
	// Its removal's approval turns 2-step verification off (2026-11-25: "2fa
	// on" and "2단계 인증 켜기" found no enroll, its name saying no switch).
	"POST /account/mfa/totp/enroll": {"2단계 인증 켜기: 인증 앱 등록 시작", "Enable 2-step verification: start enrolling an authenticator"},
	"GET /organizations/{organizationId}/account-history/{userId}": {"조직 계정의 기록 읽기(로그인 포함)",
		"Read an account’s history in an organization, logins included"},
	"PUT /workspaces/{workspaceId}/pages/{pageId}/imports/markdown/replace": {"긴 본문도 Markdown으로 통째로 바꾸기",
		"Replace a page’s whole body with Markdown, however long"},
	"POST /workspaces/{workspaceId}/pages/{pageId}/imports/markdown": {"긴 본문도 Markdown으로 끝에 붙이기",
		"Add Markdown at the end of a page’s body, however long"},
	// What a restore restores where neither its name nor its route says it:
	// a managed user is an account its organization manages, one whose
	// deletion waits coming back whole, and the server comes back from a
	// backup it has kept (2026-11-25: "restore account" and "계정 복원" put an
	// account's making first, nothing restoring an account by its name, and
	// "restore backup" said nothing does it, a replica's restore first).
	"DELETE /organizations/{organizationId}/managed-users/{userId}/deletion": {"계정 복원: 관리 대상 사용자의 기다리는 삭제 취소",
		"Restore an account: cancel a managed user’s deletion while it waits"},
	"POST /admin/backups/schedule/restore": {"백업 복원: 선택한 백업에서 서버를 다시 시작", "Restore a backup: the server starts again from the one chosen"},
	// Its configure sets a form's questions, each hidden or asked on a
	// condition (2026-11-25: "질문 숨기기", to hide a question, said nothing
	// does it once 질문 was read as a question; its name, responseCopy, says
	// only its copy).
	"POST /forms": {"양식 설정하기, 링크로 양식을 읽고 답 보내기", "Set up a form, or read one by its link and send its answers"},
	// Its usage lists each of the app's endpoints with its address and
	// whether it is open to anyone (appEndpointInfoWire) (2026-11-24: "app
	// address" and "앱 주소" put that switch's change first, no read found).
	"GET /admin/apps/installs/{installId}/usage": {"앱의 사용량과 주소 읽기", "Read an app’s usage and its addresses"},
}

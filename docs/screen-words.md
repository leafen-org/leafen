# Screen words

One word for one meaning on every screen, in Korean and in English: the
words people already know from Notion's screens, and for what Notion doesn't
have, from ChatGPT and Claude. Leafen's own things keep the names chosen for
them. Nothing here is coined.

The table is read by `scripts/check-screen-words.mjs`, which the web build
runs: the words in the last two columns may not appear in the Korean and
English catalogs (`web/src/locales`), in what the server writes (notices,
mail, push, errors — Go string literals under `internal/`, `desktop/`,
`organization/`, `cmd/`), or in the built-in and catalog
apps' screens. A word in `/…/` is a regular expression; several are
separated by ` · `. Other languages follow English when they are filled in.

When a new screen needs a word, find it here first. A concept that isn't
here takes Notion's word, else ChatGPT's or Claude's; add its row with the
source in the same change.

| Meaning | 한국어 | English | Source | 쓰지 않는 말 | Avoid |
|---|---|---|---|---|---|
| The product | 리펜 | Leafen | Product name | `/(?<![/\w])Leafen(?! backups)/` | |
| Workspace | 워크스페이스 | workspace | Notion | 작업공간 | |
| Member | 멤버 | member | Notion | 구성원 | |
| Guest | 게스트 | guest | Notion help, sharing and permissions | 손님 | |
| Owner | 소유자 | owner | Notion help, sharing and permissions | `/(?<![가-힣])주인/` | |
| Outside an organization or workspace | 외부 | external | Notion help, sharing and permissions | `/(?<![가-힣])바깥/` | `/\boutside (?:emails?\|services?\|addresses)\b/` |
| Access | 액세스 | access | Notion share menu, "액세스 권한" | 접근 권한 · `/(?<![가-힣])접근(?![가-힣])/` · 사용 권한 토큰 | |
| Allow | 허용 | allow | Notion share menu, "편집 허용" | 허락 | |
| Decline | 거부 | decline | Notion help, sharing and permissions | 거절 | |
| Full access | 전체 허용 | Full access | Notion share menu (the help pages also say 모두 허용; the menu's word is kept) | 모두 허용 · 전체 권한 | |
| Server | 서버 | server | Leafen runs as one server | 인스턴스 | `/\b[Ii]nstance\b/` |
| The server's address reachable from anywhere | 공개 주소 | public address | Built-in tunnel decision | 외부 주소 | `/\b[Oo]utside address/` |
| Where a sign-in provider sends people back | 리디렉션 URI | redirect URI | Google Cloud console, "승인된 리디렉션 URI" | 돌아올 주소 · 리디렉션 URL | `/\breturn address/` · `/\b[Rr]edirect URL/` |
| What holds an agent to its folder | 샌드박스 | sandbox | Claude Code and Codex docs | 울타리 | `/\bfence/` |
| An API's address | 엔드포인트 | endpoint | OpenAI and Anthropic API docs | 끝점 | |
| Encrypt | 암호화 | encrypt | Common usage | 봉인 | `/\b[Ss]eal(?:ed\|s)?\b/` |
| Version | 버전 | version | Notion, "버전 기록" | `/(?<![가-힣])판(?:이\|을\|은\|의\|으로\|에서\|과\|도)?(?![가-힣])/` | |
| Download | 다운로드 | download | Notion | 내려받 | |
| Upload | 업로드 | upload | Notion | `/(?:파일\|사진\|이미지)[을를]? 올리/` | |
| An open thread's reply box, before it is typed in | 회신… | Reply… | Notion's reply box (replies themselves stay 답글) | | |
| Install | 설치 | install | Common usage | `/(?<![가-힣])깔(?:기\|고\|린\|아\|면\|려\|았)/` | |
| Import | 가져오기 | import | Notion | 들여오 | `/\bbring(?:s\|ing)? (?:it \|them )?(?:in\|over)\b/` · `/\bbrought (?:in\|over)\b/` |
| Paste | 붙여넣기 | paste | Notion | 붙여 넣 | |
| Choose | 선택 | select (a thing: Select page, Select all) · choose (which, how, who …; a file; a new password or name) | Notion (Select 2–3 times as often before a thing, Choose before which/how) | `/(?<![가-힣])(?:고르\|골라\|고른)/` | |
| Edit | 편집 | edit | Notion | `/(?<![가-힣])(?:고치\|고쳐)/` | |
| Refresh | 새로고침 | refresh | Notion | 새로 고침 | |
| Test (a mail, an organization) | 테스트 | test | Common usage | 시험 | `/\(trial\)/` |
| Migration from another tool | 마이그레이션 | migration | Common usage | `/(?<![가-힣])이사(?:를\|가\|는\|의\|로\| 보고서\| 때)/` | |
| Scan | 스캔 | scan | Common usage | `/(?<![가-힣])훑/` | |
| Line break | 줄바꿈 | line break | Notion | 줄 바꿈 | |
| Cancel | 취소 | cancel, canceled | Notion (American spelling) | | `/[Cc]ancell(?:ed\|ing)/` · `/\b[Cc]all(?:ed)? off\b/` |
| Revoke a token, key, password, link or access | 철회 | revoke | Notion: personal access tokens, SCIM tokens, roles and access ("철회") | | `/\b[Tt]ake(?:s)? back\b/` · `/\btaken back\b/` |
| Get into Leafen with an account, and leave it | 로그인 · 로그아웃 | log in, log out, logged in; login (the noun) | Notion's login page ("Log in") and account menu ("Log out"); Google's, Microsoft's, GitHub's and Apple's own buttons and Claude's connector labels keep their words, and a page of another service drawn as it is carries "screen words: another service's" | | `/(?<![-/_.\w])[Ss]ign(?:s\|ed\|ing)?\s+(?:in\|out)\b(?!\s+(?:now\b\|when needed\|(?:with\|to\|of)(?: the)? (?:Google\|Microsoft\|GitHub\|Apple)\b))/` · `/(?<![-/_.\w(]\|setup \|Google )(?:(?<!^No )\|(?!sign-in$))[Ss]ign-(?:in\|out)s?(?![-/_\w)]\| <)/` · `/(?<![-/_.\w])[Ss]igned-(?:in\|out)\b/` |
| End a session | 세션 종료 | end (a session), log out | Notion, "세션 종료", "Log out" | | |
| Verify an email, a domain, a code, a webhook or a page | 인증 | verify | Notion: "이메일 인증", "도메인 인증", "코드 인증", "인증 토큰", "페이지 인증" (a backup's or a file's check stays 검증) | | |
| Suspend a user | 일시 정지 | suspend | Notion, managed users: "사용자 일시 정지" | | |
| A page's role | 읽기 허용 · 댓글 허용 · 편집 허용 · 전체 허용 | Can view · Can comment · Can edit · Full access | Notion share menu | | |
| Trash, the place deleted pages wait in | 휴지통 | Trash, a name without "the": in Trash, to Trash | Notion | | |
| A chat's message or answer | 메시지 · 응답 | message · response | ChatGPT and Claude | `/(?<![가-힣])턴(?![가-힣])/` | `/\bturn\(s\)/` · `/\bturns from\b/` · `/\b(?:its\|each\|next\|a) turn\b/` · `/\btaken-back turns\b/` |
| Question | 질문 | question | ChatGPT and Claude | 물음 | |
| Context window | 컨텍스트 | context | Claude Code | 맥락 · 문맥 | |
| Automatically | 자동으로 | automatically | Common usage | 저절로 | `/\b(?:starts\|connects\|opens\|updates?) by itself\b/` |
| Snapshot | 스냅샷 | snapshot | Common usage | 스냅숏 | |
| Database view | 보기 | view | Notion | `/(?<![A-Za-z가-힣])뷰(?![가-힣])/` | |
| Connection | 연결 | connection | Notion, "연결" | 연동 | |
| Legal hold | 법적 보존 | legal hold | Notion security docs, "법적 보존" | 법적 홀드 | |
| Audit log | 감사 로그 | audit log | Notion | 감사 기록 · 감사로그 | |
| Custom emoji | 사용자 지정 이모지 | custom emoji | Notion's emoji picker, "사용자 지정" | 사용자 정의 이모지 · 커스텀 이모지 | |
| Directory | 디렉토리 | directory | Notion settings › 일반 › 사람, "사용자 디렉토리" | 디렉터리 | |
| 2-step verification | 2단계 인증 | 2-step verification | Notion | 2차 인증 | `/\b[Tt]wo-step\b/` |
| A connection that keeps no data | 데이터를 남기지 않음(ZDR) | zero data retention (ZDR) | OpenRouter and OpenAI docs | | `/\bkeeps? (?:nothing\|no data)\b/` · `/\bkeeping (?:nothing\|no data)\b/` |
| Put a backup's content back | 복원 | restore | Notion (휴지통 복원 / Restore) and the server's own 백업에서 복원 | 되살리 | |
| The one-time code that lets a new server's first administrator in | 설정 코드 | setup code | Leafen's own first setup screen | 설치 코드 | `/\binstall(?:ation)? code\b/` |
| Where the server writes what it prints (a container's or a terminal's) | 서버 로그 | server log | Leafen's own first setup screen | 컨테이너 로그 | `/\bcontainer log\b/` |
| Get the server's content again after a restore | 서버에서 다시 불러오기 | Reload from server | Common usage | 새로 받기 | `/\banew\b/` |
| Take over an agent's browser; pass work on | 직접 제어 · 전환 | take control · pass on | OpenAI Operator, "Take control" | 넘겨받 | `/\b[Hh]and(?:s\|ed\|ing)? over\b/` · `/\b[Hh]and(?:s\|ed)? (?:it \|the screen )?back\b/` · `/\bhands (?:you\|them) its\b/` · `/\bhanded on\b/` |
| A click | 클릭 한 번 | one click | Common usage | | `/\bone press\b/` |
| Match imported people to accounts | 매핑 | match | Notion import, "매핑" | `/(?<![가-힣])대응(?:하\|해\|했\|할)/` | `/\b(?:not )?paired\b/` · `/\bPair each\b/` |
| A second server that takes over | 예비 서버 | standby server | Decision, standby server | | |
| An export of many files | 묶음 | bundle | Notion export, "Markdown & CSV" bundle | | |
| How long ago a page or block was edited | 지금 편집 · 〈N〉분 전 편집 · 〈N〉시간 전 편집 · 1일 전 편집 · 〈날짜〉 편집 | Edited just now · Edited 5m ago · Edited 1d ago · Edited Oct 24 | Notion top bar (2026-10-28) | 방금 편집됨 · `/전 편집[됨함]/` · `/\}\} 편집[됨함]/` | |
| A page, row or database with no title | 새 페이지 · 새 데이터베이스 | New page · New database | Notion: sidebar, breadcrumb, page block, page mention, tab title; an inline database's title ("새 데이터베이스") | | |

// Korean copy writes English "page" as "페이지" inside sentences and
// "{{name}}님" without a space (2026-10-16) but where Notion's same line spaces
// it (SPACED_AS_NOTION), and a renamed word keeps a
// particle that fits it (2026-10-17: "멤버이", "워크스페이스으로" left by a
// rename). Which word stands for which thing is docs/screen-words.md's,
// checked by scripts/check-screen-words.mjs.
// Statements end in "-습니다" as most of Notion's Korean does — questions and
// asks keep "-시겠어요?" and "-하세요" — and a particle after a value only known
// on screen is written as the pair the screens show, as Notion's ("{{title}}을(를)",
// lib/koreanParticles.ts, 2026-11-17); a value is quoted “ ” (2026-10-19).
// A line let off a rule below is one whose short name or particle stays as
// Notion's same line writes it; the long lines once let off as Notion's
// words are Leafen's own since 2026-11-21 and follow the rules, and a line
// named below that no longer breaks the rule it is let off fails the check,
// so the lists hold only what they still excuse.
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const directory = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'locales', 'ko')
const rules = [
  [/(?:\}\}|<\/name>) 님/u, 'attach "님" to the name'],
  [/(?<![A-Za-z_\-./{<])page(?![A-Za-z_\-}>])/u, 'write "페이지"'],
  [/(?:워크스페이스|멤버|페이지|소유자)(?:이나|으로|이|을|은|과)(?![가-힣])/u, 'a word ending in a vowel takes 가·를·는·와·로·나'],
  [/리펜(?:가|를|는|와|로|나)(?![가-힣])/u, '"리펜" takes 이·을·은·과·으로·이나'],
  [/(?:어요|아요|해요|돼요|에요|예요|와요|려요|줘요|여요|애요|커요|내요|가요|눠요|겨요|꿔요|져요|혀요|나요|봐요|둬요)(?=[.!]?(?:\s|$|[(:"”)\]]))/u, 'end a statement in "-습니다"'],
  [/(?:\}\}|<\/[a-z]+>)(?:을|를|이|가|은|는|과|와|으로|로)(?=[\s.,!?)\]…"”]|$)/u, 'write the particle pair ("을(를)", "(으)로") after a value'],
  [/"\{\{\w+\}\}"/u, 'quote a value with “ ”'],
]

// A copy to the clipboard says "… 복사됨", as Notion's "링크 복사됨" does
// (2026-10-26: "복사됨", "복사했습니다", "링크를 복사했습니다.", "복사함" and "복사 완료" for
// the same thing); the import's finished line copies a workspace, not to the
// clipboard, and a property's copy button says Notion's own "속성 복사 완료"
// word for word (seen 2026-11-03), as do a block's "클립보드에 블록 링크 복사
// 완료" and the chosen blocks' "링크 복사 완료" (seen 2026-11-06), and a code
// block's copy toast "코드 복사 완료" (seen 2026-11-07), and the clipboard
// snackbars for a link and a page's contents, "링크 복사 완료" and "페이지 내용 복사
// 완료" (Notion's language file, read 2026-11-08), and a view's "보기 링크 복사
// 완료" (seen 2026-11-09), an MCP token page's copy "클립보드에 토큰 복사
// 완료" (seen 2026-11-09), a teamspace's "초대 링크 복사" "링크 복사 완료"
// (seen 2026-11-09), and an image block's "이미지 복사" "클립보드에 이미지 복사
// 완료" (seen 2026-11-11), and a connection's share window's "링크 복사", whose
// snackbar is the same "링크 복사 완료" (Notion's developer screen code,
// 2026-11-16 collection).
const copied = /복사(?:했습니다|함|\s?완료)(?=[.!]?(?:\s|$))/u
const copiedElsewhere = new Set(['importDialog.archive.copied', 'propertyCell.valueCopied',
  'blockHandle.menu.copiedBlockLink', 'blockHandle.notifications.copiedLinks', 'blockItem.code.copied',
  'rowMenu.toast.copiedContents', 'commentsPanel.toast.linkCopied',
  'topBar.copiedLink', 'publicSiteView.linkCopied', 'workspaceSettingsDialog.inviteLinkCopied',
  'blockHandle.database.copiedLink', 'selectionToolbar.linkEditor.copiedLink',
  'databaseView.dataSourceIdCopied', 'databaseView.copiedViewLink',
  'workspaceSettingsDialog.developerTools.mcpToken.copied', 'teamspaceDialog.inviteDialog.linkCopied',
  'blockHandle.media.copiedImage', 'workspaceSettingsDialog.developerTools.internal.share.linkCopied'])
// A path to a setting goes "설정 → 계정", as Notion's help writes it
// (2026-10-26: "→" and "›" both stood for it).
const pathStep = / › /u

// Values Notion itself quotes " " in a short name, kept as it writes them
// (2026-11-11: the "+" and "[[" menus' "새 "자식" 페이지 추가", beside "새
// “자식” 하위 페이지").
const QUOTED_AS_NOTION = new Set(['blockItem.mention.addPageNamed'])
const straightQuote = rules.find(([, advice]) => advice === 'quote a value with “ ”')[0]
// Values followed by one fixed particle, as Notion's same line fixes it
// (2026-11-18: the block handle's tip "클릭하거나⌘/로 메뉴 열기", its key
// always ending in "/"; 2026-11-19, Notion's language file and screen: the
// language change question's "{language}로" and the version restore
// panel's "{version}으로 복원") — every other line after a value keeps the pair.
const PARTICLE_AS_NOTION = new Set(['blockHandle.tip.menu',
  'workspaceSettingsDialog.languageChangeConfirm', 'updatesPanel.versions.restoreTo'])
const particlePair = rules.find(([, advice]) => advice.startsWith('write the particle pair'))[0]
// Lines that space a person's name from "님" where Notion's same line does
// (user decision 2026-10-28; Notion's ko file
// writes some "{name} 님" and some "{name}님", seen 2026-11-15): the trashed
// page's band, the share list's role tips, the personal task list's name, a
// person's pages heading and its empty box's line, and the access and
// membership request lines. The inbox's rows and the version history's
// changes space it too, as Notion's do (2026-11-16: its comment and viewed
// page rows have pairs that space it, and its inbox lines space a name from
// "님" 46 times to 3, its version history lines 37 to 11; the history's
// "속성을 변경함" stays joined, as its pair is). The question before a
// teamspace member is removed spaces it too (2026-11-18, two tabs: "june lee
// 님을 … 삭제하시겠습니까?").
const SPACED_AS_NOTION = new Set([
  'updatesPanel.inbox.sentences.comment', 'updatesPanel.inbox.sentences.pageAccess', 'updatesPanel.inbox.sentences.link',
  'updatesPanel.inbox.sentences.pageEdit', 'updatesPanel.inbox.sentences.teamspaceJoinRequest', 'updatesPanel.inbox.sentences.teamspaceJoinApproved',
  'updatesPanel.versions.changes.typeChanged', 'updatesPanel.versions.changes.renamed', 'updatesPanel.versions.changes.rowAdded',
  'updatesPanel.versions.changes.rowRemoved', 'updatesPanel.versions.changes.edited', 'updatesPanel.versions.changes.editedBlock',
  'updatesPanel.versions.changes.pageEdited',
  'trashView.rowBanner.movedToTrash', 'topBar.rowTipFullAccess', 'topBar.rowTipEdit', 'topBar.rowTipComment', 'topBar.rowTipView',
  'myTasks.personalDatabasePlaceholder', 'peopleProfiles.pagesOf', 'peopleProfiles.noPages', 'workspaceSettingsDialog.requestPage_view',
  'workspaceSettingsDialog.requestPage_comment', 'workspaceSettingsDialog.requestPage_edit', 'workspaceSettingsDialog.requestPage_full_access',
  'workspaceSettingsDialog.requestGuestText', 'workspaceSettingsDialog.requestMemberText', 'workspaceSettingsDialog.requestMembershipText',
  'teamspaceDialog.memberList.removeQuestion',
])
const attachedName = rules.find(([, advice]) => advice === 'attach "님" to the name')[0]

const failures = []
const seen = new Map()
function visit(value, path) {
  if (typeof value === 'string') {
    seen.set(path, value)
    for (const [pattern, advice] of rules) {
      if (pattern === straightQuote && QUOTED_AS_NOTION.has(path)) continue
      if (pattern === particlePair && PARTICLE_AS_NOTION.has(path)) continue
      if (pattern === attachedName && SPACED_AS_NOTION.has(path)) continue
      if (pattern.test(value)) failures.push(`${path}: ${advice}: ${value.slice(0, 80)}`)
    }
    if (copied.test(value) && !copiedElsewhere.has(path)) failures.push(`${path}: say a copy as "… 복사됨": ${value.slice(0, 80)}`)
    if (pathStep.test(value)) failures.push(`${path}: write a path to a setting with " → ": ${value.slice(0, 80)}`)
    return
  }
  if (value && typeof value === 'object') {
    for (const [key, child] of Object.entries(value)) visit(child, `${path}.${key}`)
  }
}

for (const file of readdirSync(directory).filter((name) => name.endsWith('.json') && !name.startsWith('_')).sort()) {
  visit(JSON.parse(readFileSync(join(directory, file), 'utf8')), file.replace(/\.json$/u, ''))
}

// A line named above that is gone, or that no longer breaks the rule it is
// let off, is taken off its list: left there, it would let a later line under
// its name skip the rule unseen.
for (const [names, pattern] of [[QUOTED_AS_NOTION, straightQuote], [PARTICLE_AS_NOTION, particlePair],
  [SPACED_AS_NOTION, attachedName], [copiedElsewhere, copied]]) {
  for (const path of names) {
    const value = seen.get(path)
    if (value === undefined) failures.push(`${path}: no such Korean line — take it off this check's list`)
    else if (!pattern.test(value)) failures.push(`${path}: no longer needs letting off — take it off this check's list`)
  }
}

if (failures.length > 0) {
  console.error(`Korean copy rules failed:\n${failures.join('\n')}`)
  process.exit(1)
}
console.log('Korean copy rules verified.')

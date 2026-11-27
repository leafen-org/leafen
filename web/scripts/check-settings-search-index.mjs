#!/usr/bin/env node

// Every row a settings section draws is found by the settings search
// (components/settings/settingsWindowSearch.ts): a row the index leaves out
// can't be found, and a hand-kept index falls behind the rows (2026-11-06:
// 107 rows, from "선호하는 이름" to "앱 설치"; 2026-11-07: "백업 폴더", and
// the rows under 법적 보존). This check reads the names the settings' sources
// draw a row under — `<strong>{t("…")}</strong>` and the `title` of
// SettingRow, SettingField, SettingSwitch and SettingRowGroup — and stops the
// build when one is neither in the index nor left out below with its reason.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')
const DEFAULT_NAMESPACE = 'workspaceSettingsDialog'

// What the settings draw that isn't a setting to find, by why.
const LEFT_OUT = {
  // A value read out, not a setting: last and next runs, counts, versions.
  readout: [
    'serverConsole:backup.lastFile', 'serverConsole:backup.keptFile', 'serverConsole:schedule.followsPrimary', 'serverConsole:schedule.kept',
    'serverConsole:schedule.lastAttempt', 'serverConsole:schedule.lastSuccess', 'serverConsole:schedule.next', 'serverConsole:schedule.openedTitle',
    'serverConsole:report.files', 'serverConsole:report.sessions', 'serverConsole:report.users', 'serverConsole:report.workspaces',
    'serverConsole:updates.available', 'serverConsole:standby.versionPair', 'serverConsole:apps.usage.endpointPublic',
    'workspaceAnalytics:viewsCount', 'workspaceSettingsDialog:usageExpiredRow', 'workspaceSettingsDialog:groupsEmptyTitle', 'workspaceSettingsDialog:groupsNoMatchTitle',
    'workspaceSettingsDialog:move.movedPeopleTitle', 'workspaceSettingsDialog:move.movedPublicTitle',
    'firstInstall:agentsConnected',
  ],
  // An action shown only while a saved secret is there to clear.
  clearSecret: [
    'serverConsole:email.clearPassword', 'serverConsole:placeSearch.clear', 'serverConsole:providers.clearSecret',
    'networkProxy:clearPassword',
  ],
  // A step of a form being filled in (a token, a key, a webhook, an app account, two-step setup).
  formStep: [
    'workspaceSettingsDialog:enterCodeTitle', 'workspaceSettingsDialog:scanQrTitle', 'workspaceSettingsDialog:webhookSecretTitle',
    'workspaceSettingsDialog:apiKeyTitle', 'workspaceSettingsDialog:connectionApps.open', 'workspaceSettingsDialog:keyExpiryLabel',
    'workspaceSettingsDialog:keyScopeLabel', 'workspaceSettingsDialog:newManualTokenTitle', 'workspaceSettingsDialog:appAccountName',
    'workspaceSettingsDialog:agents.instructionTitle', 'workspaceSettingsDialog:agents.kind.app', 'workspaceSettingsDialog:agents.promptTitle',
    'workspaceSettingsDialog:agents.thisComputer', 'workspaceSettingsDialog:agents.otherComputer',
    'workspaceSettingsDialog:personalAccessTokenExpiryLabel',
  ],
  // One person's or one organization's own panel, opened from a list.
  detailPanel: [
    'workspaceSettingsDialog:managedUserDelete', 'workspaceSettingsDialog:managedUserDeletionScheduled',
    'workspaceSettingsDialog:managedUserEdit', 'workspaceSettingsDialog:managedUserExternal',
    'workspaceSettingsDialog:managedUserPasswordReset', 'workspaceSettingsDialog:managedUsersLogout',
    'workspaceSettingsDialog:enterpriseMcpWorkspace',
  ],
  // The 연결 section's "관리" view, which the section opens to by its own button.
  manageView: [
    'workspaceSettingsDialog:automationWebhooksField', 'workspaceSettingsDialog:customMcpAllowlistField',
    'workspaceSettingsDialog:customMcpServersField', 'workspaceSettingsDialog:tokenCreatorsField',
  ],
}

function sources(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) files.push(...sources(path))
    else if (name.endsWith('.tsx') && !/(Dialog|Sheet|Popover|Menu)\.tsx$/u.test(name)) files.push(path)
  }
  return files
}

const settingsSources = [
  ...sources(join(sourceRoot, 'components', 'settings')),
  ...sources(join(sourceRoot, 'components', 'account')),
  ...sources(join(sourceRoot, 'components', 'admin')),
  ...sources(join(sourceRoot, 'organization')),
  ...readdirSync(join(sourceRoot, 'components'))
    .filter((name) => /^WorkspaceSettings.*\.tsx$/u.test(name) || name === 'NotionAdminApiPanel.tsx')
    .map((name) => join(sourceRoot, 'components', name)),
]

const index = readFileSync(join(sourceRoot, 'components', 'settings', 'settingsWindowSearch.ts'), 'utf8')
const indexed = new Set([...index.matchAll(/"((?:[A-Za-z]+:)?[\w.]+)"/gu)]
  .map(([, key]) => (key.includes(':') ? key : `${DEFAULT_NAMESPACE}:${key}`)))
const leftOut = new Set(Object.values(LEFT_OUT).flat())

const missing = []
for (const path of settingsSources) {
  const text = readFileSync(path, 'utf8')
  const namespace = /useTranslation\(\[?"(\w+)"/u.exec(text)?.[1] ?? DEFAULT_NAMESPACE
  const names = [
    ...text.matchAll(/<strong>\{t\(\s*"([\w.:]+)"/gu),
    ...text.matchAll(/<Setting(?:Row|Field|Switch|RowGroup)\s+title=\{t\(\s*"([\w.:]+)"/gu),
  ].map(([, key]) => (key.includes(':') ? key : `${namespace}:${key}`))
  for (const name of new Set(names)) {
    if (!indexed.has(name) && !leftOut.has(name)) missing.push(`${relative(webRoot, path)}: ${name}`)
  }
}

if (missing.length > 0) {
  console.error('Settings rows the settings search can\'t find — add each to settingsWindowSearch.ts (with what draws it, if a choice does),')
  console.error('or leave it out in scripts/check-settings-search-index.mjs with its reason:')
  for (const line of missing) console.error(`  ${line}`)
  process.exit(1)
}

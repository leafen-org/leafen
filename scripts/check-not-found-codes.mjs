import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// A 404 names what wasn't found by its code, and the error's target takes
// its kind from that code (internal/app/api_error_details.go): a code that
// names one kind beside a message that names another sends whoever reads it
// to look for the wrong thing (2026-11-16: "Group not found." came as
// LEAFEN_WORKSPACE_NOT_FOUND). This checks every 404 the server writes with
// a code and a message written out: a word of the code's kind is in the
// message.
//
//   node scripts/check-not-found-codes.mjs

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sources = ['internal/app', 'organization'].map((folder) => join(productRoot, folder))

// Messages that say what is missing in other words than the code's kind.
const allowed = new Set([
  'LEAFEN_ORGANIZATION_AUDIT_NOT_FOUND No history to show.',
  'LEAFEN_APP_NOT_FOUND Not found.',
  'LEAFEN_APP_NOT_FOUND No such hook.',
  'LEAFEN_APP_NOT_FOUND No such schedule runs for you.',
  'LEAFEN_MFA_FACTOR_NOT_FOUND Authentication settings not found.',
  'LEAFEN_ROW_SEND_NOT_FOUND Not found.',
  'LEAFEN_EVENT_WEBHOOK_NOT_FOUND No failed delivery to retry.',
  'LEAFEN_INTERACTIVE_AUTOMATION_NOT_FOUND Button, template or target content not found.',
  'LEAFEN_INTERACTIVE_AUTOMATION_NOT_FOUND Database not found.',
  'LEAFEN_INTERACTIVE_AUTOMATION_NOT_FOUND Workspace not found.',
  'LEAFEN_MCP_SERVICE_NOT_FOUND No such connection.',
  'LEAFEN_NOTION_IMPORT_CONNECTION_NOT_FOUND Source not found.',
  'LEAFEN_NOTION_IMPORT_NOT_FOUND File result not found.',
  'LEAFEN_PAGE_ANALYTICS_NOT_FOUND The workspace was not found.',
  'LEAFEN_FILE_POSTER_NOT_FOUND The video has no still yet.',
  'LEAFEN_JOB_NOT_FOUND Import not found.',
  'LEAFEN_JOB_NOT_FOUND Wiki change not found.',
  'LEAFEN_JOB_NOT_FOUND Comment deletion not found.',
  'LEAFEN_WORKSPACE_ACTIVITY_MISSING This update is no longer in the list.',
  'LEAFEN_ORGANIZATION_DISCOVERY_NOT_FOUND Search export not found.',
])

const constants = new Map()
const files = []
for (const folder of sources) {
  for (const name of readdirSync(folder).filter((file) => file.endsWith('.go') && !file.endsWith('_test.go')).sort()) {
    const text = readFileSync(join(folder, name), 'utf8')
    files.push([join(folder, name), text])
    for (const match of text.matchAll(/\b(errorCode\w+|ErrorCode\w+)\s*=\s*"([A-Z_]+)"/g)) constants.set(match[1], match[2])
  }
}
for (const [, text] of files) {
  for (const match of text.matchAll(/\b(ErrorCode\w+)\s*=\s*(errorCode\w+)\b/g)) {
    if (constants.has(match[2])) constants.set(match[1], constants.get(match[2]))
  }
}
const problems = []
for (const [path, text] of files) {
  for (const match of text.matchAll(/http\.StatusNotFound,\s*(?:leafen\.)?(\w+),\s*"((?:[^"\\]|\\.)*)"/g)) {
    const code = constants.get(match[1])
    if (!code || !/_(NOT_FOUND|MISSING)$/.test(code) || allowed.has(`${code} ${match[2]}`)) continue
    const words = code.replace(/^LEAFEN_/, '').replace(/_(NOT_FOUND|MISSING)$/, '').toLowerCase().split('_')
    if (!words.some((word) => match[2].toLowerCase().includes(word))) {
      const line = text.slice(0, match.index).split('\n').length
      problems.push(`${path.slice(productRoot.length + 1)}:${line}: ${code} with "${match[2]}"`)
    }
  }
}
if (problems.length) {
  console.error(`404s whose code names another kind than their message (scripts/check-not-found-codes.mjs):\n${problems.join('\n')}`)
  process.exit(1)
}

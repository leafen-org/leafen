// English screen text writes Notion's typographic marks: an apostrophe is ’
// ("You’re", "{{name}}’s"), a quoted word or value “ ” (2026-11-19 — Notion's
// English lines write ’ 3,498 times to 35 straight and quote with “ ” 630
// times to 6; Leafen's own lines wrote the typewriter ' and " in about 1,280
// places, so one screen showed both). A straight mark stays only in what a
// person types or copies as it is — a command, a header, a JSON body, a
// formula (CODE_QUOTED).
// A copy to the clipboard is said "Copied …", as Notion's "Copied link to
// clipboard" (2026-10-26); a line that says it after the thing ("Link
// copied", "… copied to clipboard") stays only where Notion's same place
// says those words (COPIED_AS_NOTION), and a failure ("couldn’t be copied")
// is no confirmation (2026-11-22: the connection share's "Link copied" came
// in beside Notion's "Copied link to clipboard" for the same button, seen by
// the gate's test only).
import { readdirSync, readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const directory = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'locales', 'en')

// Lines whose straight " is code: the agent text's JSON field and header for
// another agent and its JSON-RPC request body, the device code command, the
// restore API body and the recipient formula.
const CODE_QUOTED = new Set([
  'agentPrompt.otherAgent', 'agentPrompt.httpHow', 'authGate.deviceCodeNoDeviceCommand',
  'authGate.setupRestoreAgentPromptApi', 'databaseView.recipientFormulaHelp',
  'workspaceSettingsDialog.apiKeyDesc',
])

// Lines whose "… copied" is Notion's same line word for word: the signed-in
// link's and the search preview's "Link copied" (Notion's
// searchResultCard2.linkCopied and its search preview, 2026-11-14) and a data
// source's "{{name}} ID copied" (manageDataSourcesMenu.uuidCopied, Notion's
// en-US language file, 2026-11-08).
const COPIED_AS_NOTION = new Set(['authGate.signedInLink.copied', 'databaseView.dataSourceIdCopied',
  'searchDialog.highlights.copiedLink'])
const copiedAfter = /\bcopied(?: to (?:the )?clipboard)?\.?$/u
const copyFailure = /\b(?:couldn[’']t|could not|can[’']t|cannot)\b/u
const saysCopiedAfter = (value) => copiedAfter.test(value) && !copyFailure.test(value) && !/\bCopied\b/u.test(value)

const failures = []
const seen = new Set()
const quoted = new Set()
const lines = new Map()
function visit(value, path, key) {
  if (typeof value === 'string') {
    seen.add(key)
    lines.set(path, value)
    if (value.includes('"')) quoted.add(key)
    if (saysCopiedAfter(value) && !COPIED_AS_NOTION.has(path)) {
      failures.push(`${path}: say a copy as “Copied …” (“Copied link to clipboard”): ${value.slice(0, 80)}`)
    }
    if (value.includes("'")) {
      failures.push(`${path}: write an apostrophe as ’ and a quote as “ ” or ‘ ’: ${value.slice(0, 80)}`)
    }
    if (value.includes('"') && !CODE_QUOTED.has(key)) {
      failures.push(`${path}: quote a word or a value with “ ”: ${value.slice(0, 80)}`)
    }
    return
  }
  if (Array.isArray(value)) {
    value.forEach((child, index) => visit(child, `${path}[${index}]`, key))
  } else if (value && typeof value === 'object') {
    for (const [name, child] of Object.entries(value)) visit(child, `${path}.${name}`, key === path ? `${key}.${name}` : key)
  }
}

for (const file of readdirSync(directory).filter((name) => name.endsWith('.json') && !name.startsWith('_')).sort()) {
  const namespace = file.replace(/\.json$/u, '')
  visit(JSON.parse(readFileSync(join(directory, file), 'utf8')), namespace, namespace)
}

// A named line that is gone (renamed or removed), or that no longer breaks
// the rule it is let off (no straight " any more, no "… copied"), would let a
// later line under its name skip the rule unseen, so every name above must
// still be a line that needs letting off.
for (const key of CODE_QUOTED) {
  if (!seen.has(key)) failures.push(`${key}: no such English line — take it off this check's list`)
  else if (!quoted.has(key)) failures.push(`${key}: holds no straight " — take it off this check's list`)
}
for (const path of COPIED_AS_NOTION) {
  if (!lines.has(path)) failures.push(`${path}: no such English line — take it off this check's list`)
  else if (!saysCopiedAfter(lines.get(path))) failures.push(`${path}: no longer says a copy after the thing — take it off this check's list`)
}

if (failures.length > 0) {
  console.error(`English copy rules failed:\n${failures.join('\n')}`)
  process.exit(1)
}
console.log('English copy rules verified.')

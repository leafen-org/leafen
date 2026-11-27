#!/usr/bin/env node

// A failure's own message is not for the screen: a request's is the
// transport's English with a diagnostic reference ("Leafen request failed
// (409).", "Leafen server response unavailable."), a script's is a
// developer's. Screens say what failed in the reader's words through
// userErrorText or userErrorToast (src/lib/settingsErrors.ts), which also
// leave an unreachable server to the page's connection bar. The files below
// read a message only to tell failures apart, to log it, or to show authors
// the mistake in their own equation or diagram, and say so.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { relativeSlashPath } from '../../scripts/source-paths.mjs'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')

const READS_MESSAGE = new Map([
  ['src/main.tsx', 'the packaged client failing to start before the app and its languages load'],
  ['src/components/database/formulaCore.ts', 'a formula\'s own error, named on screen by its key'],
  ['src/components/apps/AppCredentialDialog.tsx', 'telling a refused credential apart'],
  ['src/components/apps/appFrameBridge.ts', 'the answer to an app\'s own call, not shown'],
  ['src/components/editor/fileDrop.ts', 'logged only'],
  ['src/components/editor/HtmlEmbedBlock.tsx', 'telling HTML turned off apart'],
  ['src/components/editor/richtextEquation.ts', 'the author\'s equation\'s mistake'],
  ['src/components/editor/MermaidPreview.tsx', 'the author\'s diagram\'s mistake'],
  ['src/lib/fileUrls.ts', 'telling a network failure apart'],
  ['src/lib/serverKeyPin.ts', 'telling a certificate the server didn\'t sign apart from an unreachable server'],
])

// `error instanceof Error ? error.message`, and `.message : String(error)`.
const RAW = [/\b([\w.]+) instanceof Error \? \1\.message\b/u, /\.message : String\([\w.]+\)/u]

function sourceFiles(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) {
      if (name !== 'locales' && name !== 'node_modules') files.push(...sourceFiles(path))
    } else if (/\.(ts|tsx)$/u.test(name) && !/\.(test|spec)\.tsx?$/u.test(name) && !name.endsWith('.d.ts')) {
      files.push(path)
    }
  }
  return files
}

const found = []
for (const path of sourceFiles(sourceRoot)) {
  const file = relativeSlashPath(webRoot, path)
  if (READS_MESSAGE.has(file)) continue
  readFileSync(path, 'utf8').split('\n').forEach((line, index) => {
    if (RAW.some((pattern) => pattern.test(line))) found.push(`${file}:${index + 1} ${line.trim()}`)
  })
}

if (found.length) {
  console.error(`A failure's own message reaches the screen (${found.length}); say what failed with userErrorText or userErrorToast (src/lib/settingsErrors.ts):\n${found.join('\n')}`)
  process.exit(1)
}

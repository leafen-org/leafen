#!/usr/bin/env node

// A menu's or a small window's edge is the ring at the end of --shadow-menu,
// as Notion's (seen 2026-10-30: no border, a 0 0 0 1px shadow). A border
// drawn as well doubles the edge and moves every row 1px in from Notion's.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

function styleFiles(directory) {
  const files = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) files.push(...styleFiles(path))
    else if (name.endsWith('.css')) files.push(path)
  }
  return files
}

const found = []
for (const file of styleFiles(join(webRoot, 'src'))) {
  const text = readFileSync(file, 'utf8')
  for (const rule of text.matchAll(/([^{}]+)\{([^{}]*)\}/gu)) {
    if (rule[2].includes('var(--shadow-menu)') && /(^|;|\s)border:\s*[^;]*\b(solid|dashed|dotted|double)\b/u.test(rule[2])) {
      const line = text.slice(0, rule.index + rule[1].length).split('\n').length
      found.push(`${relative(webRoot, file)}:${line} ${rule[1].trim().split('\n').pop()}`)
    }
  }
}
if (found.length) {
  console.error('A rule with --shadow-menu also draws a border; its ring is the edge:')
  for (const place of found) console.error(`  ${place}`)
  process.exit(1)
}

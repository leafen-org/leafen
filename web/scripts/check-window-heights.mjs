#!/usr/bin/env node

// Heights follow the window a person sees: an iPhone home screen app measures
// 100vh as the whole screen with its status bar (874px for an 812px window),
// so the app ran past it, the page scrolled and the sidebar's inbox badge and
// the chat box were cut. The build writes every style sheet's vh length as
// dvh (postcss.config.mjs, and shipped-bundle-rules.mjs checks what ships);
// a style a script writes says dvh itself, which this check asks of every
// string in the app's scripts. A comment naming Notion's "80vh" is no style.

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

function files(directory) {
  const found = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) found.push(...files(path))
    else if (/\.tsx?$/u.test(name) && !name.includes('.generated.')) found.push(path)
  }
  return found
}

const failures = []
for (const path of files(join(webRoot, 'src'))) {
  const lines = readFileSync(path, 'utf8').split('\n')
  lines.forEach((line, index) => {
    if (/^\s*\/\//u.test(line)) return
    const match = /["'`][^"'`\n]*\dvh\b/u.exec(line)
    if (match) failures.push(`${relative(webRoot, path)}:${index + 1}: ${match[0].slice(0, 80)}`)
  })
}

if (failures.length) {
  console.error('Scripts write heights in vh (an iPhone home screen app measures it past its window):')
  for (const failure of failures) console.error(`  ${failure}`)
  console.error('Write dvh.')
  process.exit(1)
}

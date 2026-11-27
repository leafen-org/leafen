#!/usr/bin/env node

// A link or a label drawn as a button keeps its words in its middle. A
// <button> sets its line in its middle by itself; a link or a label of a
// fixed height sets it at its top, so a class given both had the link's words
// high (2026-11-10: "앱에서 열기" beside "홈 화면에 추가"). A class with a fixed
// height of a button's size on a link or a label with words in it composes
// ButtonLine's line, or centres its line itself (flex or grid, or a line
// height as tall as it is).

import { readdirSync, readFileSync, statSync } from 'node:fs'
import { dirname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const sourceRoot = join(webRoot, 'src')

function files(directory, suffix) {
  const found = []
  for (const name of readdirSync(directory)) {
    const path = join(directory, name)
    if (statSync(path).isDirectory()) {
      if (name !== 'node_modules' && name !== 'locales') found.push(...files(path, suffix))
    } else if (name.endsWith(suffix)) found.push(path)
  }
  return found
}

// Each module's classes whose own rule fixes a button's height and leaves
// its line where it falls.
const uncentred = new Map()
for (const file of files(sourceRoot, '.module.css')) {
  const text = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//gu, '')
  const classes = new Map()
  for (const rule of text.matchAll(/([^{}@]+)\{([^{}]*)\}/gu)) {
    const body = rule[2]
    for (const selector of rule[1].split(',')) {
      const name = /^\s*\.([A-Za-z0-9_]+)\s*$/u.exec(selector)?.[1]
      if (!name) continue
      const entry = classes.get(name) ?? { height: 0, centred: false }
      const height = /(?<![-\w])height:\s*(\d+(?:\.\d+)?)px/u.exec(body)
      if (height) entry.height = Number(height[1])
      const lineHeight = /line-height:\s*(\d+(?:\.\d+)?)px/u.exec(body)
      if (/composes:\s*line\s+from\s+"[^"]*ButtonLine\.module\.css"/u.test(body) ||
        /display:\s*(inline-)?(flex|grid)/u.test(body) || /align-content:\s*center/u.test(body) ||
        (lineHeight && height && lineHeight[1] === height[1])) entry.centred = true
      classes.set(name, entry)
    }
  }
  const names = new Set([...classes].filter(([, entry]) => entry.height >= 16 && entry.height <= 44 && !entry.centred).map(([name]) => name))
  if (names.size) uncentred.set(file, names)
}

const found = []
for (const file of files(sourceRoot, '.tsx')) {
  const text = readFileSync(file, 'utf8')
  const modules = new Map()
  for (const [, variable, path] of text.matchAll(/import (\w+) from "([^"]+\.module\.css)"/gu)) {
    modules.set(variable, path.startsWith('@/') ? join(sourceRoot, path.slice(2)) : resolve(dirname(file), path))
  }
  for (const element of text.matchAll(/<(a|label)\b[^>]*?className=\{([^}]*)\}[^>]*>\s*(\{t\(|[^<{\s])/gu)) {
    for (const [, variable, name] of element[2].matchAll(/(\w+)\.([A-Za-z0-9_]+)/gu)) {
      if (uncentred.get(modules.get(variable))?.has(name)) {
        const line = text.slice(0, element.index).split('\n').length
        found.push(`${relative(webRoot, file)}:${line} <${element[1]}> ${variable}.${name}`)
      }
    }
  }
}

if (found.length > 0) {
  console.error('A link or a label drawn as a button sets its words at its top; compose ButtonLine\'s line in its class:')
  for (const place of found) console.error(`  ${place}`)
  process.exit(1)
}

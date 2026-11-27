import { spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

// The avatar icons (contracts/avatar-icons.json) are pictures a person can
// pick instead of a photo or their first letter: Phosphor's glyphs,
// the set the app's own icons come from, so they look like one family
// (2026-10-16). The contract names each icon and its Phosphor glyph and keeps
// the glyph's drawing in the weights the app shows; --from-phosphor reads the
// drawings again from the installed package. Until 2026-11-09 each person's
// new account was given one; since then nobody is given one and a person
// without a photo shows their first letter, as in Notion. The server keeps
// the names to refuse others and serves each as an image for the API; the
// web app draws them in the text color, a weight for each size. An icon
// marked "retired" is no longer offered, yet still drawn for those who have
// it: pictures shaped like the app's own buttons (the bell, the star, the
// pencil and others) went that way on 2026-11-06.

const scriptsRoot = dirname(fileURLToPath(import.meta.url))
const productRoot = resolve(scriptsRoot, '..')
const phosphorRoot = resolve(productRoot, 'web/node_modules/@phosphor-icons/react')

// Thin to bold: the lines grow 8, 12, 16 and 24 of the glyph's 256.
const WEIGHTS = ['thin', 'light', 'regular', 'bold']
// The web app's first screens need the small sizes' weights; the large
// sizes' come when a large avatar shows.
const MAIN_WEIGHTS = ['regular', 'bold']
const FINE_WEIGHTS = ['thin', 'light']
// The API's image, mostly shown small by those who read it.
const IMAGE_WEIGHT = 'regular'

function parseArgs(argv) {
  const options = {
    check: false,
    fromPhosphor: false,
    contract: resolve(productRoot, 'contracts/avatar-icons.json'),
    goOutput: resolve(productRoot, 'internal/app/avatar_icons.generated.go'),
    tsOutput: resolve(productRoot, 'web/src/avatarIcons.generated.ts'),
    fineTsOutput: resolve(productRoot, 'web/src/avatarIconFineWeights.generated.ts'),
  }
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index]
    if (argument === '--check') {
      options.check = true
      continue
    }
    if (argument === '--from-phosphor') {
      options.fromPhosphor = true
      continue
    }
    const key = {
      '--contract': 'contract',
      '--go-output': 'goOutput',
      '--ts-output': 'tsOutput',
      '--fine-ts-output': 'fineTsOutput',
    }[argument]
    if (!key || !argv[index + 1]) throw new Error(`unknown or incomplete argument: ${argument}`)
    options[key] = resolve(productRoot, argv[index + 1])
    index += 1
  }
  return options
}

const namePattern = /^[a-z]{2,24}$/u
const glyphPattern = /^[A-Z][A-Za-z]{1,40}$/u
const pathPattern = /^[MmLlHhVvCcSsQqTtAaZz][MmLlHhVvCcSsQqTtAaZz0-9 ,.-]*$/u

function assertIconList(contract) {
  if (contract?.schemaVersion !== 2 || contract?.name !== 'avatar-icons' || !contract.version) {
    throw new Error('avatar icon contract must use schemaVersion 2 and a version')
  }
  if (!Array.isArray(contract.icons) || contract.icons.filter(({ retired }) => !retired).length < 2) throw new Error('avatar icons are missing')
  const names = contract.icons.map(({ name }) => name)
  if (new Set(names).size !== names.length) throw new Error('avatar icon names contain duplicates')
  for (const icon of contract.icons) {
    // "letter" names a person's first letter, not a picture.
    if (!namePattern.test(icon.name) || icon.name === 'letter' || !glyphPattern.test(icon.phosphor ?? '')
      || Object.keys(icon).some((key) => !['name', 'phosphor', 'retired', 'weights'].includes(key))
      || ('retired' in icon && icon.retired !== true)) {
      throw new Error(`avatar icon ${icon.name} is invalid`)
    }
  }
}

function assertContract(contract) {
  assertIconList(contract)
  if (!/^\d+\.\d+\.\d+$/u.test(contract.phosphorVersion ?? '')) throw new Error('avatar icons need the Phosphor version they come from')
  for (const icon of contract.icons) {
    const weights = icon.weights ?? {}
    if (Object.keys(weights).join() !== WEIGHTS.join()) throw new Error(`avatar icon ${icon.name} needs the weights ${WEIGHTS.join(', ')}`)
    for (const weight of WEIGHTS) {
      const paths = weights[weight]
      if (!Array.isArray(paths) || paths.length === 0 || paths.some((d) => typeof d !== 'string' || !pathPattern.test(d))) {
        throw new Error(`avatar icon ${icon.name} has an invalid ${weight} path`)
      }
    }
  }
}

// A Phosphor glyph's drawing in a weight: the paths of its React elements.
function elementPaths(element, paths = []) {
  if (!element || typeof element !== 'object') return paths
  if (Array.isArray(element)) {
    for (const child of element) elementPaths(child, paths)
    return paths
  }
  if (typeof element.type === 'string' && element.type !== 'path') {
    throw new Error(`Phosphor glyph draws a ${element.type}, which avatar icons don't carry`)
  }
  if (element.type === 'path') paths.push(element.props.d)
  return elementPaths(element.props?.children, paths)
}

async function drawFromPhosphor(contract) {
  const phosphorVersion = JSON.parse(readFileSync(resolve(phosphorRoot, 'package.json'), 'utf8')).version
  const icons = []
  for (const { name, phosphor, retired } of contract.icons) {
    const module = await import(pathToFileURL(resolve(phosphorRoot, 'dist/defs', `${phosphor}.es.js`)).href)
    const drawings = Object.values(module).find((value) => value instanceof Map)
    if (!drawings) throw new Error(`Phosphor glyph ${phosphor} has no drawings`)
    const weights = {}
    for (const weight of WEIGHTS) weights[weight] = elementPaths(drawings.get(weight))
    icons.push({ name, phosphor, ...(retired ? { retired } : {}), weights })
  }
  const { icons: _icons, phosphorVersion: _version, ...head } = contract
  return { ...head, phosphorVersion, icons }
}

function renderGo(contract, digest) {
  const lines = [
    '// Code generated by scripts/generate-avatar-icons.mjs. DO NOT EDIT.',
    `// Contract SHA-256: ${digest}`,
    '',
    'package app',
    '',
    `// avatarIconPaths holds each icon's drawing, filled, on a 256 grid (Phosphor's ${IMAGE_WEIGHT} weight),`,
    '// a retired one\'s too, for those who have it.',
    'var avatarIconPaths = map[string][]string{',
  ]
  for (const icon of contract.icons) {
    lines.push(`\t${JSON.stringify(icon.name)}: {${icon.weights[IMAGE_WEIGHT].map((d) => JSON.stringify(d)).join(', ')}},`)
  }
  lines.push('}')
  const formatted = spawnSync('gofmt', [], { input: `${lines.join('\n')}\n`, encoding: 'utf8' })
  if (formatted.status !== 0) throw new Error(formatted.stderr || 'gofmt failed')
  return formatted.stdout
}

function renderWeights(contract, weights) {
  const lines = []
  for (const icon of contract.icons) {
    lines.push(`  ${icon.name}: {`)
    for (const weight of weights) lines.push(`    ${weight}: [${icon.weights[weight].map((d) => JSON.stringify(d)).join(', ')}],`)
    lines.push('  },')
  }
  return lines
}

function renderTypeScript(contract, digest) {
  const lines = [
    '// Code generated by scripts/generate-avatar-icons.mjs. DO NOT EDIT.',
    `// Contract SHA-256: ${digest}`,
    '',
    `/** Each avatar icon's drawing, filled, on Phosphor's 256 grid: its ${MAIN_WEIGHTS.join(' and ')} weights, a retired one's too. */`,
    'export const AVATAR_ICONS = {',
    ...renderWeights(contract, MAIN_WEIGHTS),
    `} as const satisfies Record<string, Record<${MAIN_WEIGHTS.map((weight) => JSON.stringify(weight)).join(' | ')}, readonly string[]>>;`,
    '',
    'export type AvatarIconName = keyof typeof AVATAR_ICONS;',
    '/** The icons offered to choose from, in picker order. */',
    `export const AVATAR_ICON_NAMES: readonly AvatarIconName[] = [${contract.icons.filter(({ retired }) => !retired).map(({ name }) => JSON.stringify(name)).join(', ')}];`,
  ]
  return `${lines.join('\n')}\n`
}

function renderFineTypeScript(contract, digest) {
  const lines = [
    '// Code generated by scripts/generate-avatar-icons.mjs. DO NOT EDIT.',
    `// Contract SHA-256: ${digest}`,
    '',
    `/** Each avatar icon's ${FINE_WEIGHTS.join(' and ')} weights, for large avatars. */`,
    'export const AVATAR_ICON_FINE_WEIGHTS = {',
    ...renderWeights(contract, FINE_WEIGHTS),
    `} as const satisfies Record<string, Record<${FINE_WEIGHTS.map((weight) => JSON.stringify(weight)).join(' | ')}, readonly string[]>>;`,
  ]
  return `${lines.join('\n')}\n`
}

function checkOrWrite(path, expected, check) {
  if (check) {
    let actual = ''
    try {
      actual = readFileSync(path, 'utf8')
    } catch (error) {
      if (error?.code !== 'ENOENT') throw error
    }
    if (actual !== expected) {
      process.stderr.write(`stale generated output: ${relative(productRoot, path)}\n`)
      process.exitCode = 1
    }
    return
  }
  mkdirSync(dirname(path), { recursive: true })
  writeFileSync(path, expected)
}

try {
  const options = parseArgs(process.argv.slice(2))
  let contractSource = readFileSync(options.contract, 'utf8')
  let contract = JSON.parse(contractSource)
  if (options.fromPhosphor) {
    assertIconList(contract)
    contract = await drawFromPhosphor(contract)
    assertContract(contract)
    contractSource = `${JSON.stringify(contract, null, 2)}\n`
    checkOrWrite(options.contract, contractSource, options.check)
  }
  assertContract(contract)
  const digest = createHash('sha256').update(contractSource).digest('hex')
  checkOrWrite(options.goOutput, renderGo(contract, digest), options.check)
  checkOrWrite(options.tsOutput, renderTypeScript(contract, digest), options.check)
  checkOrWrite(options.fineTsOutput, renderFineTypeScript(contract, digest), options.check)
} catch (error) {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`)
  process.exitCode = 1
}

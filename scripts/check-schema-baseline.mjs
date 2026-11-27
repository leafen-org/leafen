// A new data folder takes the schema baseline (internal/app/schema_baseline.go)
// in one step, as the migration it names, and every later migration after
// it. The baseline stays as it is: a new migration changes nothing in it.
// This check fails when the baseline file isn't the one the Go code names —
// its format, version or the digest of the migrations it holds — or when
// the migration list doesn't start with it. Whether it still fits this
// build's PocketBase shows at a new data folder's first start, and with
//
//   go run ./cmd/leafen-schema-baseline
import { readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  readNativeSchemaMigrationDefinitionsSource,
  readNativeSchemaMigrationImpactLedger,
} from './generate-schema-migration-impact.mjs'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appRoot = join(root, 'internal', 'app')
const baselinePath = join(appRoot, 'schema_baseline.json')

function fail(message) {
  process.stderr.write(`${message}\n`)
  process.exit(1)
}

const goSource = readFileSync(join(appRoot, 'schema_baseline.go'), 'utf8')
const constant = (name) => goSource.match(new RegExp(`\\b${name}\\s*=\\s*("?)([0-9a-f]+)\\1`, 'u'))?.[2]
const format = Number(constant('schemaBaselineFormat'))
const version = Number(constant('schemaBaselineVersion'))
const checksum = constant('schemaBaselineChecksum')
if (!format || !version || !checksum) fail('internal/app/schema_baseline.go no longer names the baseline\'s format, version and checksum')

let baseline
try {
  baseline = JSON.parse(readFileSync(baselinePath, 'utf8'))
} catch (error) {
  fail(`internal/app/schema_baseline.json doesn't read: ${error.message}`)
}
if (baseline.format !== format || baseline.version !== version || baseline.migrations !== checksum) {
  fail(`internal/app/schema_baseline.json is not the baseline schema_baseline.go names (format ${format}, version ${version}): ` +
    'it stays as it is; a new migration goes at the end of the migration list instead')
}
if (!Array.isArray(baseline.replaced) || baseline.replaced.length === 0 || !Array.isArray(baseline.create) || baseline.create.length === 0) {
  fail('internal/app/schema_baseline.json lacks what the baseline creates or what of PocketBase\'s it replaces')
}
const first = readNativeSchemaMigrationImpactLedger(readNativeSchemaMigrationDefinitionsSource())[0]
if (first?.version !== version || first?.checksumIdentifier !== 'schemaBaselineChecksum') {
  fail(`the migration list doesn't start with the schema baseline, version ${version}`)
}
process.stdout.write(`schema baseline ${version}: ${baseline.create.length} objects\n`)

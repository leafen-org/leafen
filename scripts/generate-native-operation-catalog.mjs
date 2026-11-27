import { readdirSync, readFileSync, writeFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

// What the wire contracts say about the native API's operations, for the
// native MCP's operation list: each operation's contract and name, and the
// records (field names and types) its requests and responses are made of, and
// the values its named kinds take.
// The server reads it from internal/app; the routes themselves come from
// the router, so an operation without a contract is still listed.
//
//   node scripts/generate-native-operation-catalog.mjs [--check]

const productRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const contractsRoot = join(productRoot, 'contracts')
const target = join(productRoot, 'internal', 'app', 'native_operation_catalog.generated.json')

// Routes whose contract describes their records without listing them as
// operations.
const contractOfRoute = {
  'POST /pages/{id}/content-graph/mutations': { contract: 'content-graph', name: 'mutateContentGraph' },
  'GET /pages/{id}/content-graph/changes': { contract: 'content-graph', name: 'readContentGraphChanges' },
}

const operations = { ...contractOfRoute }
const contracts = {}
for (const file of readdirSync(contractsRoot).filter((name) => name.endsWith('.json')).sort()) {
  const contract = JSON.parse(readFileSync(join(contractsRoot, file), 'utf8'))
  const name = file.replace(/-wire\.json$|\.json$/u, '')
  const records = {}
  for (const record of contract.records ?? []) {
    // A field the server alone keeps (goOnly) is not on the wire.
    const fields = (record.fields ?? []).filter((field) => !field.goOnly)
      // What 0 means where it isn't none (a period of 0 days keeps everything),
      // and an old name kept on the wire, what it was.
      .map((field) => ({
        name: field.jsonName, type: field.tsType ?? field.goType,
        ...(field.zeroMeans ? { zeroMeans: field.zeroMeans } : {}), ...(field.deprecated ? { deprecated: field.deprecated } : {}),
      }))
    if (fields.length) records[record.tsName ?? record.goName] = fields
  }
  // A contract's union of operations (the content graph's) reads as one
  // record: its kind, and the field each kind carries, a named alias of a
  // record (one written with a field left out) spelled out.
  const aliases = Object.fromEntries((contract.valueAliases ?? []).map((alias) => [alias.tsName, alias.tsType]))
  const variants = contract.operation?.variants ?? []
  if (variants.length) {
    records[contract.operation.tsName ?? contract.operation.goName] = [
      { name: 'kind', type: variants.map((variant) => JSON.stringify(variant.kind)).join(' | ') },
      ...variants.map((variant) => ({
        name: variant.jsonField,
        type: `${aliases[variant.tsType] ?? variant.tsType ?? variant.goType} (with kind ${JSON.stringify(variant.kind)})`,
      })),
    ]
  }
  const listed = contract.operations ?? (contract.operation?.method ? [contract.operation] : [])
  if (!listed.length && !Object.values(contractOfRoute).some((route) => route.contract === name)) continue
  const enums = {}
  for (const entry of contract.enums ?? []) {
    const values = (entry.values ?? []).map((item) => item.value ?? item)
    if (values.length) enums[entry.tsName ?? entry.goName] = values
  }
  // Records go by these wire names alone: what a handler reads and answers
  // is named so too (scripts/native-operation-inputs, wire_names.go).
  contracts[name] = { records, ...(Object.keys(enums).length ? { enums } : {}) }
  for (const operation of listed) {
    if (!operation.method || !operation.path?.startsWith('/')) continue
    operations[`${operation.method.toUpperCase()} ${operation.path}`] = {
      contract: name, name: operation.tsName ?? operation.name ?? operation.goName,
    }
  }
}
const sorted = (object) => Object.fromEntries(Object.keys(object).sort().map((key) => [key, object[key]]))
const output = `${JSON.stringify({ operations: sorted(operations), contracts: sorted(contracts) }, null, 1)}\n`
if (process.argv.includes('--check')) {
  if (readFileSync(target, 'utf8') !== output) {
    console.error(`stale generated output: ${target}`)
    process.exit(1)
  }
} else {
  writeFileSync(target, output)
}

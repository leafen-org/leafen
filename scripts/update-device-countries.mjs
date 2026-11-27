#!/usr/bin/env node
// Brings the country data a device's place is read from up to this month's
// release of DB-IP's IP to Country Lite (https://db-ip.com, CC BY 4.0), which
// ships in the program (internal/app/device_location.go). Run it before a
// release; the data ages by the month.
//
//	node scripts/update-device-countries.mjs
import { writeFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gunzipSync } from 'node:zlib'

const product = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const month = new Date().toISOString().slice(0, 7)
const address = `https://download.db-ip.com/free/dbip-country-lite-${month}.mmdb.gz`
const response = await fetch(address)
if (!response.ok) throw new Error(`${address}: ${response.status}`)
const data = Buffer.from(await response.arrayBuffer())
const unpacked = gunzipSync(data)
if (!unpacked.includes('DBIP-Country-Lite')) throw new Error(`${address} is not the country data`)
writeFileSync(resolve(product, 'internal/app/device_countries.mmdb.gz'), data)
console.log(`${month}: ${data.length} bytes, ${unpacked.length} unpacked`)

// A connection the agent holds already, kept in place of the text's code,
// takes the default workspace the text's person chose (2026-11-23,
// internal/app/mcp_connection_kept_code.go): the commands send the code with
// that connection's own header, which never shows, to its address, and
// print Leafen's answer, which says what it has now. The code stays unused.
// The commands keep the entry's address in A and its header in K; in a
// POSIX shell K holds the whole "Authorization: …" line, in PowerShell its
// value alone.

import { PS_JSON_BODY } from "./powerShellQuoted";

const MCP_PATH = "/api/leafen/mcp";
const KEPT_CODE_PATH = "/api/leafen/v1/mcp/connections/current/code";

/** The POSIX shell's call, then what it says after. */
export function keptCodeCall(code: string, then: string): string {
  return `printf '%s\\n' "$K" | curl -sS -X POST "\${A%${MCP_PATH}}${KEPT_CODE_PATH}" -H @- -H 'Content-Type: application/json' `
    + `-d '{"code": "${code}"}'; echo; ${then}`;
}

/**
 * The same in PowerShell, $W naming the entry kept. In its double-quoted
 * words $W goes as $($W) before a colon: PowerShell reads "$W:" as a
 * variable with a scope or drive ($env:PATH), and the whole line failed to
 * parse (2026-11-25).
 */
export function psKeptCodeCall(code: string, then: string, reason: string): string {
  return `try { (Invoke-RestMethod -Method Post -Uri (($A -replace '${MCP_PATH}$', '') + '${KEPT_CODE_PATH}') `
    + `-Headers @{ Authorization = $K } ${PS_JSON_BODY} -Body '{"code": "${code}"}').message } `
    + `catch { "Leafen didn't take the code for $($W): " + ${reason} }; ${then}`;
}

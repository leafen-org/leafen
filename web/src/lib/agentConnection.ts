// What the settings' "Agents" section gives a person to connect an agent to
// Leafen (agent memory decision of 2026-10-10, 2): for each agent the
// easiest way the web can offer, written for this server's address. An
// address the internet reaches takes agents only over HTTPS; one only this
// computer reaches connects only this computer's agents.

import { i18next } from "@/i18n";
import { hostReach, outsideAddress } from "./addressReach";
import { claudeConnectorBlockingPort } from "./claudeConnector";
import { keptCodeCall, psKeptCodeCall } from "./agentConnectKept";
import { agentEntryNames, agentPersonLatinName, LIST_NAME_COMPUTER, LIST_NAME_WORDS, listNamePowerShell } from "./agentEntryNames";
import { PS_JSON_BODY, psQuoted } from "./powerShellQuoted";

export { agentEntryNames } from "./agentEntryNames";

export type AgentServerAddress = {
  origin: string;
  /** The Leafen-native MCP endpoint, which tells agents how to use Leafen. */
  mcpUrl: string;
  /** Why commands aren't given (https) or what limits them (local). */
  limit: "" | "https-required" | "local";
  /** Whether an agent on the internet (ChatGPT, Claude on the web) reaches it. */
  outside: boolean;
};

export const NATIVE_MCP_PATH = "/api/leafen/mcp";

export function agentServerAddress(origin: string): AgentServerAddress {
  let parsed: URL;
  try {
    parsed = new URL(origin);
  } catch {
    return { origin, mcpUrl: "", limit: "https-required", outside: false };
  }
  const base = parsed.origin;
  // This computer's and its network's hosts are lib/addressReach.ts's, as is outside.
  const reach = hostReach(parsed.hostname);
  const limit = parsed.protocol !== "https:" && !reach ? "https-required" : reach === "loopback" ? "local" : "";
  return { origin: base, mcpUrl: limit === "https-required" ? "" : base + NATIVE_MCP_PATH, limit, outside: outsideAddress(base) };
}

function base64(value: string): string {
  return btoa(String.fromCharCode(...new TextEncoder().encode(value)));
}

/** Install links of the editors that take one, for Leafen's entry under name (agentEntryNames). */
export function agentInstallLinks(mcpUrl: string, name = "leafen"): { cursor: string; vscode: string } {
  return {
    cursor: `cursor://anysphere.cursor-deeplink/mcp/install?name=${encodeURIComponent(name)}&config=${encodeURIComponent(base64(JSON.stringify({ url: mcpUrl })))}`,
    vscode: `vscode:mcp/install?${encodeURIComponent(JSON.stringify({ name, type: "http", url: mcpUrl }))}`,
  };
}

/**
 * The lines an agent writes into its instruction file for Leafen, the same
 * as the connect command's (internal/app/agent_connect_claude_code.go): the
 * person's own words, approved as they connect, which keep their address as
 * Leafen says it is, Leafen's answers saying only where it is now.
 */
export function agentInstructionLines(origin: string): string {
  return [
    "<!-- leafen:begin -->",
    `Leafen (${origin}) is my shared workspace and memory: search and read it before answering about my work, and save lasting knowledge there. `
      + "When Leafen says its address changed, update the address in these lines.",
    "<!-- leafen:end -->",
  ].join("\n");
}

/**
 * The one text a person pastes into any agent (the built-in tunnel decision's
 * change of 2026-10-14): Leafen's address on this computer and, when it has
 * one, from anywhere, and a one-time code. An agent that runs commands
 * connects with the code first, whatever it is (user 2026-11-19: the code
 * traded for a key was the intended road; since 2026-11-16 the text had sent
 * Claude Code and Codex to sign in through /mcp and the browser instead, and
 * the Claude app's Code tab then asked for a new session before the Notion
 * move began): one command trades the code and puts Leafen's entry straight
 * into its own settings — Claude Code's `claude mcp add-json`, Codex's
 * config.toml — so the key shows in no conversation or output
 * (internal/app/agent_connect_by_code.go), for every folder, with what the key
 * may do and until when. Only a chat app, which can't, has its person add a
 * custom connector that signs in. Then the lines for its user-wide
 * instruction file; going on in the same conversation, calling Leafen's MCP
 * over HTTP itself while Leafen's tools don't show there yet; what to start
 * with — a Notion move begins in the conversation, its Notion connection
 * made there first, never in Leafen's import screen (user 2026-11-20: "채팅
 * 내에서 엔드투엔드"; the text had sent the person to that screen for the key);
 * and how Leafen moves to one folder later. A connection the agent holds
 * already, this Leafen's, another person's or another Leafen's, stays as it
 * is: Leafen's is added beside it (agentEntryNames; user 2026-11-19: the text
 * had the agent remove the person's earlier connection to the same address
 * and add it again);
 * one Leafen refuses the key of, or an earlier one to this Leafen it won't
 * use, it asks its person once whether to remove, and only on their yes
 * disconnects it here with its own key and takes it out of its settings
 * (user 2026-11-20: it had ended with "disconnect it yourself in Settings").
 * Each command comes for a POSIX shell and for Windows PowerShell, and the
 * text says to run the one for the agent's shell as it is (user 2026-11-21:
 * Claude Code on Windows without bash rewrote the bash command itself, and
 * the key's save broke on PowerShell's quotes; agentConnectPowerShellCommands).
 * A command whose save failed runs again with the same code within its
 * minutes, while the key it gave is unused (mcp_connection_retrade.go).
 * Before trading the code, each command keeps a connection at this
 * Leafen's address, whatever its name, that works here already — a key
 * Leafen still takes for all the person's workspaces, as that person:
 * another person's in the same settings is never kept (2026-11-27) — and
 * trades nothing, the code left unused (user 2026-11-20: a text added
 * leafen-tulip-2 beside a leafen-tulip that worked; agentConnectCommands).
 * It asks how to start —
 * moving their Notion workspace here, or from an empty workspace — only
 * where the server says nothing has come into the workspace yet (offerStart,
 * internal/app/agent_start_offer.go; user 2026-11-20: an agent connected
 * later, after the move too, asked again); elsewhere it says it is connected
 * and moves nothing unasked. What it says to its person stays plain. It
 * holds no key.
 */
export function agentConnectionPrompt(input: {
  /** The address this computer reaches Leafen at, "" when it is the public one. */
  localOrigin: string;
  /** The address agents anywhere reach, "" with none. */
  publicOrigin: string;
  /**
   * The address the instruction file's lines name: the one Leafen's links take — the server's own address, else this
   * page's — as the server names it to its agents (internal/app/agent_facing_address.go); left out, the first given.
   */
  linesOrigin?: string;
  code: string;
  /** Where a code is traded, on either address. */
  exchangePath: string;
  expiresAt: number;
  /** What the agent does once connected, in place of asking how to start. */
  startWith?: string;
  /** The server's name in plain letters (agentServerLatinName) and its code, which name Leafen's entry in the agent's settings (agentEntryNames). */
  serverName?: string;
  serverCode?: string;
  /** The person the code is theirs: a connection kept must work as them, and one beside another's names them (agentEntryNames). */
  person?: { id: string; name?: string; email?: string };
  /** The day the code's key ends, as the person reads dates; "" says nothing of it. */
  keyEnds?: string;
  /**
   * Whether the person may turn on the public address themselves (the server's
   * administrator); false sends them to that administrator, as the settings do.
   */
  canTurnOnPublic?: boolean;
  /** Whether the agent asks how to start (the code's offerStart); false says it is connected and offers nothing. */
  offerStart?: boolean;
}): string {
  const minutes = Math.max(1, Math.round((input.expiresAt - Date.now()) / 60_000));
  const local = input.localOrigin && input.localOrigin !== input.publicOrigin ? input.localOrigin : "";
  // The person's language, which the agent then speaks with them (2026-10-16:
  // a Korean screen's text was English, and so were the agent's answers);
  // codes, commands, addresses and the instruction lines stay as they are.
  const t = (key: string, options?: Record<string, unknown>) => String(i18next.t(`agentPrompt:${key}`, { ...options, interpolation: { escapeValue: false } }));
  let nearby = t("onComputer");
  try {
    if (local && hostReach(new URL(local).hostname) !== "loopback") nearby = t("onNetwork");
  } catch {
    // An address that doesn't read is named as this computer's.
  }
  const addresses = [
    ...(local ? [`- ${nearby}: ${local}${NATIVE_MCP_PATH}`] : []),
    ...(input.publicOrigin ? [`- ${t("fromAnywhere")}: ${input.publicOrigin}${NATIVE_MCP_PATH}`] : []),
  ];
  const which = local && input.publicOrigin ? t("whichBoth")
    : local ? t(input.canTurnOnPublic === false ? "whichLocalAsk" : "whichLocal") : "";
  // The commands go to the first address; with two, the agent that can't
  // reach it puts the second in them.
  const first = local || input.publicOrigin;
  const exchange = `{"code": "${input.code}", "name": "${t("nameHint")}"}`;
  const names = agentEntryNames(input.serverName, input.serverCode, agentPersonLatinName(input.person));
  const reach = agentEntryReach([local, input.publicOrigin].filter(Boolean).map((origin) => origin + NATIVE_MCP_PATH), input.person?.id);
  const commands = agentConnectCommands(first, input.exchangePath, input.code, names, reach);
  const powerShell = agentConnectPowerShellCommands(first, input.exchangePath, input.code, names, reach);
  const offer = input.offerStart !== false || Boolean(input.startWith);
  const connectorPort = input.publicOrigin ? claudeConnectorBlockingPort(input.publicOrigin) : "";
  return [
    t(offer ? "intro" : "introOnly"),
    t("talk"),
    t("plain"),
    "",
    ...addresses,
    ...(which ? [which] : []),
    "",
    t("byCode", { minutes }),
    ...(local && input.publicOrigin ? [t("commandsFirst")] : []),
    t("shells"),
    `- ${t("claudeCode")}`,
    `  ${commands.claudeCode}`,
    `  ${t("inPowerShell")}`,
    `  ${powerShell.claudeCode}`,
    `- ${t("codex")}`,
    `  ${commands.codex}`,
    `  ${t("inPowerShell")}`,
    `  ${powerShell.codex}`,
    `- ${t("otherAgent", { path: input.exchangePath, json: exchange, check: agentEntryCheckPath(reach), name: names[0] })}`,
    t("alongside", { name: names[0], beside: names[1] ?? names[0], names: names.slice(1).join(", ") }),
    ...(input.keyEnds ? [t("keyLine", { date: input.keyEnds })] : []),
    // A chat app connects by a connector, which Claude's doesn't at a port (lib/claudeConnector).
    ...(input.publicOrigin ? ["", t("chatApps"), ...(connectorPort ? [t("chatAppsClaudePort", { port: connectorPort })] : [])] : []),
    "",
    // The lines go where the server went: the person's own file.
    t("instructionFile"),
    "",
    agentInstructionLines(input.linesOrigin || input.publicOrigin || local),
    "",
    // A server added during a conversation isn't in it until Claude Code's
    // person types /reload-plugins in a terminal, and without one not at all
    // (2026-11-19, measured with Claude Code 2.1.280 and 2.1.325): there the
    // agent calls Leafen's MCP over HTTP itself, in the same conversation.
    t("sameConversation"),
    `  ${commands.call}`,
    `  ${t("inPowerShell")}`,
    `  ${powerShell.call}`,
    t("httpHow", { name: names[0] }),
    "",
    // A move begins with its first words, in Leafen's words (importDialog handoff.firstWords, loaded with the text).
    input.startWith || (offer ? t("startInChat", { firstWords: String(i18next.t("importDialog:handoff.firstWords")) }) : t("connectedOnly")),
    ...(input.startWith || !offer ? [] : [t("notionMove")]),
    t("oneFolderAlongside"),
  ].join("\n");
}

/**
 * What a connection an agent holds must reach to be kept rather than a new
 * one traded: one of this Leafen's MCP addresses, and the check's query —
 * all the person's workspaces, as an agent connection reaches them, as the
 * person connecting (GET /mcp/connections/current with its key answers 204,
 * and 403 for another person's; internal/app/agent_connect_reuse.go says the
 * same).
 */
export type AgentEntryReach = { addresses: string[]; query: string };

export function agentEntryReach(addresses: string[], person = ""): AgentEntryReach {
  return {
    addresses: [...new Set(addresses.map((address) => address.replace(/\/+$/u, "")).filter(Boolean))],
    query: person ? `all=1&person=${encodeURIComponent(person)}` : "all=1",
  };
}

const OWN_CONNECTION_PATH = "/api/leafen/v1/mcp/connections/current";

/** The check's path and query, as an agent sends it to an address's origin. */
function agentEntryCheckPath(reach: AgentEntryReach): string {
  return OWN_CONNECTION_PATH + (reach.query ? `?${reach.query}` : "");
}

const shellQuoted = (value: string) => `'${value.replaceAll("'", `'\\''`)}'`;

// The check of one entry, at "$A" and named "$M", whose header `header`
// prints: kept in W when Leafen takes its key for the reach and `kept`
// holds, its name added to R when Leafen refuses it (agentEntryCheckLine in
// internal/app/agent_connect_reuse.go).
function agentEntryCheck(reach: AgentEntryReach, header: string, kept = ""): string {
  return `case "$A" in ${reach.addresses.map(shellQuoted).join("|")}) K="$(${header})"; if [ -n "$K" ]; then `
    + `H="$(printf '%s\\n' "$K" | curl -s -o /dev/null -w '%{http_code}' "\${A%${NATIVE_MCP_PATH}}${agentEntryCheckPath(reach)}" -H @-)"; `
    + `if [ "$H" = 204 ]${kept ? ` && ${kept}` : ""}; then W="$M"; break; fi; [ "$H" != 401 ] || R="$R $M"; fi;; esac; `;
}

// Each entry Claude Code lists at an address of the reach, whatever its
// name, checked by its URL and header from `claude mcp get` and kept only in
// the settings S names (claudeCodeHeldLine in agent_connect_reuse.go).
function claudeCodeHeld(reach: AgentEntryReach): string {
  if (!reach.addresses.length) return "";
  const listed = reach.addresses.map((address) => `-e ${shellQuoted(`: ${address} (HTTP)`)}`).join(" ");
  return `for M in $("$C" mcp list 2>/dev/null | grep -F ${listed} | sed -n -E 's/^ *(.*): [^ ]+ \\(HTTP\\).*$/\\1/p'); `
    + `do G="$("$C" mcp get "$M" 2>/dev/null)" || continue; A="$(printf '%s\\n' "$G" | sed -n 's/^ *URL: *//p')"; `
    + agentEntryCheck(reach, `printf '%s\\n' "$G" | sed -n 's/^ *\\(Authorization: .*\\)$/\\1/p'`, `printf '%s\\n' "$G" | grep -qi "^ *Scope: $S "`)
    + "done; ";
}

// Each table of Codex's config.toml at an address of the reach, its
// subtables read with it.
function codexHeld(reach: AgentEntryReach): string {
  if (!reach.addresses.length) return "";
  return `for M in $(sed -n 's/^\\[mcp_servers\\.\\([A-Za-z0-9_-]*\\)\\]$/\\1/p' "$F" 2>/dev/null); do `
    + `T="$(awk -v h="[mcp_servers.$M]" 'index($0, "[") == 1 { f = $0 == h || index($0, substr(h, 1, length(h) - 1) ".") == 1; next } f' "$F")"; `
    + `A="$(printf '%s\\n' "$T" | sed -n 's/^ *url *= *"\\(.*\\)" *$/\\1/p')"; `
    + agentEntryCheck(reach, `printf '%s\\n' "$T" | sed -n 's/.*Authorization"\\{0,1\\} *= *"\\([^"]*\\)".*/Authorization: \\1/p'`)
    + "done; ";
}

/**
 * The commands of the connection text at origin: Claude Code's and Codex's.
 * Each first looks at the entries its settings hold at an address of the
 * reach, whatever their names: one whose key Leafen still takes for it — in
 * Claude Code, in the settings it adds to, S (user: every folder; local: one
 * folder) — is kept, and nothing is traded, the code left unused; it takes
 * the default workspace the text chose (agentConnectKept.ts). Otherwise
 * each takes the first of names its settings don't hold yet — never
 * removing or overwriting an entry there — trades the code and hands
 * Leafen's entry straight to their settings under that name, the key in no
 * output, then names the entries at this Leafen whose key Leafen refuses;
 * with every name taken nothing is traded, so the code stays for another
 * try. Then the line that calls Leafen's MCP over HTTP with the header Claude
 * Code keeps, which goes from `claude mcp get` to curl's standard input
 * (Leafen's program says the same: claudeCodeHTTPCall in
 * internal/app/agent_connect_by_code.go). Claude Code sets
 * CLAUDE_CODE_EXECPATH in the commands it runs, in its terminal and in the
 * Claude app, which puts no `claude` on the PATH.
 */
export function agentConnectCommands(origin: string, exchangePath: string, code: string, names = agentEntryNames(),
  reach = agentEntryReach([origin + NATIVE_MCP_PATH])) {
  // Traded under the program's name, this computer's and — Claude Code kept
  // to one folder (S=local) — the folder's, as Leafen lists it (agentEntryNames.ts).
  const trade = (name: string, answer: string, folder: boolean) => LIST_NAME_COMPUTER
    + (folder ? `D=""; [ "$S" = user ] || D="$(basename "$PWD" | tr -cd 'A-Za-z0-9._-')"; ` : `D=""; `)
    + `E="$(curl -sSf '${origin}${exchangePath}' -H 'Content-Type: application/json' `
    + `-d '{"code": "${code}", "name": "${name}'"${LIST_NAME_WORDS}"'", "answer": "${answer}"}')"`;
  const refused = `{ [ -z "$R" ] || echo "Leafen refuses the key of:$R"; }`;
  // The first free name, or "" when every one is taken.
  const free = (taken: string) => `for N in ${names.join(" ")} ""; do [ -z "$N" ] || ! ${taken} && break; done; `;
  return {
    claudeCode: `C="\${CLAUDE_CODE_EXECPATH:-claude}"; S=user; W=""; R=""; ${claudeCodeHeld(reach)}`
      + `if [ -n "$W" ]; then ${keptCodeCall(code, `echo "Claude Code is connected to this Leafen already as $W, which works: nothing was traded, and the code stays unused"; `)}`
      + `else ${free(`"$C" mcp get "$N" >/dev/null 2>&1`)}if [ -z "$N" ]; then echo "Claude Code has ${names.join(", ")} already: nothing was traded"; `
      + `else ${trade("Claude Code", "claude-code", true)} && "$C" mcp add-json --scope "$S" "$N" "$E" && ${refused}; fi; fi`,
    codex: `F="\${CODEX_HOME:-$HOME/.codex}/config.toml"; W=""; R=""; ${codexHeld(reach)}`
      + `if [ -n "$W" ]; then ${keptCodeCall(code, `echo "Codex is connected to this Leafen already as $W, which works: nothing was traded, and the code stays unused"; `)}`
      + `else ${free(`grep -qs "^\\[mcp_servers\\.$N\\]" "$F"`)}if [ -z "$N" ]; then echo "Codex has ${names.join(", ")} already: nothing was traded"; `
      + `else ${trade("Codex", "codex", false)} && mkdir -p "\${F%/*}" && printf '\\n[mcp_servers.%s]\\n%s\\n' "$N" "$E" >> "$F" `
      + `&& echo "Added Leafen to Codex as $N" && ${refused}; fi; fi`,
    call: `"\${CLAUDE_CODE_EXECPATH:-claude}" mcp get ${names[0]} | sed -n 's/^ *\\(Authorization: .*\\)$/\\1/p' | curl -sS '${origin}${NATIVE_MCP_PATH}' `
      + `-H @- -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' `
      + `-d '{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "<tool>", "arguments": {…}}}'`,
  };
}

// The connection text's commands for Windows PowerShell, beside the ones for
// a POSIX shell (agentConnectCommands), with the same rules: a connection the
// agent holds at this Leafen that works is kept and nothing traded;
// otherwise Leafen goes in under the first name free, beside what is there,
// and the key shows in no output (user 2026-11-21: on a Windows PC without
// bash, Claude Code rewrote the bash command into PowerShell itself; the code
// was traded, and the key's save broke on how Windows PowerShell 5.1 passes
// quotes to a program). So nothing here hands a program JSON in an argument:
// Claude Code takes Leafen's address and header as plain arguments (`claude
// mcp add --transport http … --header`, whose output, which repeats the
// header, is kept back), Codex's table is appended to its config.toml as
// UTF-8 without passing through a program, and each request's body goes to
// PowerShell's own web commands. They run in Windows PowerShell 5.1 and
// PowerShell 7 alike. What fails says so in plain words, never as
// PowerShell's error record (psNativeText, PS_REASON). Leafen's program says
// the same for its own lines (internal/app/agent_connect_powershell.go).

const PS_START = "$ErrorActionPreference = 'Continue'; $ProgressPreference = 'SilentlyContinue'; ";

// A program's output with what it says on its error output as plain lines,
// joined by line feeds. Under 2>&1 Windows PowerShell 5.1 makes each such
// line an error record, which Out-String prints with PowerShell's own
// position and category lines (위치 줄:1 문자:… CategoryInfo …
// NativeCommandError) between the program's words and what the command says
// next (2026-11-21, a failed save on the real Claude Code 2.1.330), and
// Out-String wraps at the console's width, which could split the key the
// command then hides.
const psNativeText = (command: string) => `((${command} 2>&1 | ForEach-Object { "$_" }) -join [char]10)`;

// A caught error in plain words: what the server answered, and the innermost
// reason (a .NET call's error names the call, not why it failed).
const PS_REASON = `"$($_.ErrorDetails.Message) $($_.Exception.GetBaseException().Message)".Trim()`;

// Claude Code's own program: the one running the command (CLAUDE_CODE_EXECPATH)
// when it is Claude Code's, else claude on the PATH — its .exe or .cmd, never
// a .ps1 an execution policy may refuse.
const PS_CLAUDE = "$C = $env:CLAUDE_CODE_EXECPATH; if ($C -and (Split-Path -Leaf $C) -notlike 'claude*') { $C = '' }; "
  + "if (-not $C) { $C = (Get-Command claude -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source }; ";

// The check of one entry at $A, its Authorization header's value in $K, named
// $M: kept in $W when Leafen takes its key and kept holds, its name added to
// $R when Leafen refuses it.
function psEntryCheck(reach: AgentEntryReach, kept = ""): string {
  return `$H = 0; try { $H = [int](Invoke-WebRequest -UseBasicParsing -Uri (($A -replace ${psQuoted(`${NATIVE_MCP_PATH}$`)}, '') + `
    + `${psQuoted(agentEntryCheckPath(reach))}) -Headers @{ Authorization = $K }).StatusCode } catch { if ($_.Exception.Response) `
    + `{ $H = [int]$_.Exception.Response.StatusCode } }; if ($H -eq 204${kept ? ` -and ${kept}` : ""}) { $W = $M; break }; `
    + `if ($H -eq 401) { $R += " $M" }; `;
}

// Each entry Claude Code lists at an address of the reach, whatever its name,
// checked by the URL and header `claude mcp get` says, and kept only in the
// settings $S names (its Scope line).
function psClaudeCodeHeld(reach: AgentEntryReach): string {
  if (!reach.addresses.length) return "";
  return `$P = @(${reach.addresses.map(psQuoted).join(", ")}); foreach ($L in @(& $C mcp list 2>$null)) { `
    + `if ("$L" -match '^\\s*(.+?): (\\S+) \\(HTTP\\)' -and $P -contains $Matches[2]) { $M = $Matches[1]; `
    + `$G = (& $C mcp get $M 2>$null) -join [char]10; `
    + `if ($LASTEXITCODE -eq 0 -and $G -match '(?m)^\\s*URL:\\s*(\\S+)' -and $P -contains $Matches[1]) { $A = $Matches[1]; `
    + `if ($G -match '(?m)^\\s*Authorization:\\s*(.+?)\\s*$') { $K = $Matches[1]; `
    + psEntryCheck(reach, `$G -match "(?mi)^\\s*Scope:\\s*$S "`) + "} } } }; ";
}

// Each table of Codex's config.toml ($Q, its lines) at an address of the
// reach, its subtables read with it.
function psCodexHeld(reach: AgentEntryReach): string {
  if (!reach.addresses.length) return "";
  return `$P = @(${reach.addresses.map(psQuoted).join(", ")}); `
    + `foreach ($M in @($Q | ForEach-Object { if ($_ -match '^\\[mcp_servers\\.([A-Za-z0-9_-]*)\\]$') { $Matches[1] } })) { `
    + `$I = $false; $A = ''; $K = ''; foreach ($L in $Q) { if ($L.StartsWith('[')) { $I = ($L -eq "[mcp_servers.$M]") -or `
    + `$L.StartsWith("[mcp_servers.$M.") } elseif ($I) { if ($L -match '^\\s*url\\s*=\\s*"(.*)"\\s*$') { $A = $Matches[1] }; `
    + `if ($L -match 'Authorization"?\\s*=\\s*"([^"]*)"') { $K = $Matches[1] } } }; `
    + `if ($K -and $P -contains $A) { ${psEntryCheck(reach)}} }; `;
}

const psRefused = `if ($R) { "Leafen refuses the key of:$R" }`;

/**
 * Claude Code's and Codex's commands for Windows PowerShell, and the line that
 * calls Leafen's MCP over HTTP with the header Claude Code keeps — what
 * agentConnectCommands gives for a POSIX shell.
 */
export function agentConnectPowerShellCommands(origin: string, exchangePath: string, code: string, names: string[],
  reach: AgentEntryReach) {
  const free = (taken: string) => `$N = ''; foreach ($T in @(${names.map(psQuoted).join(", ")})) { ${taken} }; `;
  // Traded under the program's, this computer's and (Claude Code with $S
  // local) the folder's name, as the POSIX commands do.
  const trade = (name: string, answer: string, folder: string) => `$E = $null; try { $E = Invoke-RestMethod -Method Post -Uri `
    + `${psQuoted(origin + exchangePath)} ${PS_JSON_BODY} -Body (${psQuoted(`{"code": "${code}", "name": "`)} + `
    + `${listNamePowerShell(name, folder)} + ${psQuoted(`", "answer": "${answer}"}`)}) } catch { "Leafen didn't trade the code: " + `
    + `${PS_REASON} }; `;
  const again = psQuoted(" Run this same command again: within the code's minutes Leafen trades it once more while the key it gave is unused.");
  return {
    claudeCode: PS_START + PS_CLAUDE + "$S = 'user'; $W = ''; $R = ''; "
      + "if (-not $C) { 'Claude Code''s program (claude) is not found here: nothing was traded, and the code stays unused' } else { "
      + psClaudeCodeHeld(reach)
      + `if ($W) { ${psKeptCodeCall(code, `"Claude Code is connected to this Leafen already as $W, which works: nothing was traded, and the code stays unused"`, PS_REASON)} } `
      + `else { ${free("& $C mcp get $T *> $null; if ($LASTEXITCODE -ne 0) { $N = $T; break }")}`
      + `if (-not $N) { ${psQuoted(`Claude Code has ${names.join(", ")} already: nothing was traded`)} } `
      + `else { ${trade("Claude Code", "claude-code", "$S -ne 'user'")}if ($E) { $V = $E.headers.Authorization; `
      + `$O = ${psNativeText("& $C mcp add --transport http --scope $S $N $E.url --header \"Authorization: $V\"")}.Replace($V, '<key>'); `
      + `if ($LASTEXITCODE -eq 0) { "Added Leafen to Claude Code as $N"; ${psRefused} } `
      + `else { "Claude Code didn't add Leafen: $O" + ${again} } } } } }`,
    // A config.toml that doesn't read trades nothing; one that doesn't take
    // Leafen's table says so, never that it was added.
    codex: PS_START + "$D = $env:CODEX_HOME; if (-not $D) { $D = Join-Path $HOME '.codex' }; $F = Join-Path $D 'config.toml'; "
      + "$W = ''; $R = ''; $Q = @(); $Z = ''; try { if (Test-Path -LiteralPath $F) { $Q = @(Get-Content -LiteralPath $F -ErrorAction Stop) } } "
      + `catch { $Z = ${PS_REASON} }; if ($Z) { "Codex's config.toml doesn't read ($Z): nothing was traded, and the code stays unused" } else { `
      + psCodexHeld(reach)
      + `if ($W) { ${psKeptCodeCall(code, `"Codex is connected to this Leafen already as $W, which works: nothing was traded, and the code stays unused"`, PS_REASON)} } `
      + `else { ${free('if (-not ($Q | Where-Object { $_.StartsWith("[mcp_servers.$T]") })) { $N = $T; break }')}`
      + `if (-not $N) { ${psQuoted(`Codex has ${names.join(", ")} already: nothing was traded`)} } `
      + `else { ${trade("Codex", "codex", "")}if ($E) { try { New-Item -ItemType Directory -Force -Path $D -ErrorAction Stop | Out-Null; `
      + `[IO.File]::AppendAllText($F, "$([char]10)[mcp_servers.$N]$([char]10)$E"); "Added Leafen to Codex as $N"; ${psRefused} } `
      + `catch { "Codex didn't add Leafen: " + ${PS_REASON} + ${again} } } } } }`,
    call: PS_START + PS_CLAUDE + "if (-not $C) { 'Claude Code''s program (claude) is not found here: nothing was sent' } else { try { "
      + `$K = ((& $C mcp get ${psQuoted(names[0])}) -match '^\\s*Authorization:' | Select-Object -First 1) -replace `
      + "'^\\s*Authorization:\\s*', ''; $B = [Text.Encoding]::UTF8.GetBytes("
      + `'{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "<tool>", "arguments": {…}}}'); `
      + `$V = Invoke-WebRequest -UseBasicParsing -Method Post -Uri ${psQuoted(origin + NATIVE_MCP_PATH)} -Headers @{ Authorization = $K; `
      + `Accept = 'application/json, text/event-stream' } ${PS_JSON_BODY} -Body $B; `
      + "[Text.Encoding]::UTF8.GetString($V.RawContentStream.ToArray()) } catch { \"The call to Leafen's MCP failed: \" + "
      + `${PS_REASON} } }`,
  };
}

/** The key a made connection's URI carries: what a client sends as its bearer. */
export function connectionKey(uri: string) {
  const fragment = uri.split("#")[1] ?? "";
  return new URLSearchParams(fragment).get("key") ?? "";
}

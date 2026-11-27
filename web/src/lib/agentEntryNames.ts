// Leafen's entry in an agent's settings is named after the server, the same
// in every folder and on every computer, so rules, skills and allow lists
// that name its tools (mcp__leafen-maple-kdr__…) hold everywhere (user
// 2026-11-27): leafen-<server>-<code>, its name in plain letters and its
// code (from the server's own ID, kept through backups, restores and moves),
// so two servers of one name differ — leafen-<code> when the name has no
// plain letters. Only where the settings hold that name already — another
// person's connection on the same computer account, an earlier one — does it
// go in beside it as leafen-<server>-<code>-<person>, the person named by the
// first word of their name in plain letters (else of their email, else their
// account). Nothing is numbered and nothing random goes in it. Each name
// keeps Claude's tool names, mcp__<entry>__<tool>, within 64 characters for
// Leafen's longest tool, cutting the server's name alone. Leafen says the
// same (internal/app/agent_entry_names.go).

/** Claude's limit on a tool's name, and Leafen's longest tool's name (notion-query-multiple-data-sources). */
const TOOL_NAME_LIMIT = 64;
const LONGEST_TOOL_NAME = 34;
/** What that leaves an entry's name: mcp__ + name + __ + tool. */
const ENTRY_NAME_ROOM = TOOL_NAME_LIMIT - "mcp__".length - "__".length - LONGEST_TOOL_NAME;
/** A person's part is at most this long, so with the code it always fits. */
const PERSON_ROOM = 12;

const cut = (name: string, room: number) => name.slice(0, Math.max(room, 0)).replace(/-+$/u, "");

/** The first run of plain letters and digits in text, lowercase, its accents left off. */
function latinWord(text: string): string {
  return /[a-z0-9]+/u.exec(text.normalize("NFD").toLowerCase().replace(/\p{Mn}+/gu, ""))?.[0] ?? "";
}

/** How an entry beside another names its person: their name's first word, else their email's, else their account's ID. */
export function agentPersonLatinName(person?: { id?: string; name?: string; email?: string }): string {
  if (!person) return "";
  for (const text of [person.name ?? "", (person.email ?? "").split("@")[0], person.id ?? ""]) {
    const word = latinWord(text);
    if (word) return word;
  }
  return "";
}

/**
 * The server's part of the names: its name in plain letters, as the server
 * makes it (serverLatinName in internal/app/server_name.go), none when it has
 * none, and the alias it was made with when it has no name.
 */
export function agentServerLatinName(server: { name: string; alias: string }): string {
  if (!server.name.trim()) return server.alias;
  return server.name.toLowerCase().replace(/[^a-z0-9]+/gu, "-").replace(/^-+|-+$/gu, "").slice(0, 30).replace(/-+$/u, "");
}

/**
 * The names Leafen's entry may take in an agent's settings, tried in order:
 * leafen-<server>-<code> (agentServerLatinName; the code lowercase), then,
 * with a person, leafen-<server>-<code>-<person>.
 */
export function agentEntryNames(serverName = "", serverCode = "", person = ""): string[] {
  const server = serverName.toLowerCase().replace(/[^a-z0-9-]+/gu, "").replace(/^-+|-+$/gu, "");
  const named = (tail: string) => {
    const part = cut(server, ENTRY_NAME_ROOM - "leafen".length - tail.length - 1);
    return part ? `leafen-${part}${tail}` : `leafen${tail}`;
  };
  const code = serverCode ? `-${serverCode.toLowerCase()}` : "";
  const word = cut(latinWord(person), PERSON_ROOM);
  return [named(code), ...(word ? [named(`${code}-${word}`)] : [])];
}

// A connection made by the text's commands is listed in Leafen by its
// program, the computer the command runs on (uname -n, its first part;
// Environment.MachineName in PowerShell) and, kept to one folder, that
// folder, each in plain letters after " · " — written in the trade's JSON as
// ·, so no shell or PowerShell changes it (agentConnectionListName in
// internal/app/agent_entry_names.go).

/** The POSIX shell that sets U to this computer's name. */
export const LIST_NAME_COMPUTER = `U="$(uname -n 2>/dev/null | sed 's/[.].*//' | tr -cd 'A-Za-z0-9_-')"; `;

/** The words that add U (and D, a folder's name) to a JSON string, inside the shell's double quotes. */
export const LIST_NAME_WORDS = "${U:+ \\u00b7 $U}${D:+ \\u00b7 $D}";

/**
 * The same name in PowerShell, as a JSON string's words: program, this
 * computer and, where folder holds, the folder it runs in (folder is a
 * PowerShell condition).
 */
export function listNamePowerShell(program: string, folder = ""): string {
  const parts = [`'${program}'`, "([Environment]::MachineName -replace '[^A-Za-z0-9_-]', '')",
    ...(folder ? [`$(if (${folder}) { (Split-Path -Leaf (Get-Location).Path) -replace '[^A-Za-z0-9._-]', '' })`] : [])];
  return `((@(${parts.join(", ")}) | Where-Object { $_ }) -join ' \\u00b7 ')`;
}

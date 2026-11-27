// PowerShell's single-quoted string, which takes a value as it is, in Windows
// PowerShell 5.1 and PowerShell 7 alike: each character PowerShell reads as a
// single quote — the ASCII one and the typographic ‘ ’ ‚ ‛, which end the
// string as it does — is doubled, as PowerShell's own escaping does, so a
// name or folder with ’ in it stays inside the string instead of leaving the
// rest of the line unparsed (2026-11-25). Leafen's program quotes the same
// (internal/commandline/powershell.go).
export function psQuoted(value: string): string {
  return `'${value.replace(/['‘’‚‛]/gu, (quote) => quote + quote)}'`;
}

// The type a request's JSON body goes with: Windows PowerShell 5.1 sends a
// body whose type names no charset as ISO-8859-1, and a name in it
// ("June’s 시험") reached Leafen as "June's ??" (2026-11-25); with
// charset=utf-8 it goes as UTF-8, in PowerShell 7 too (psJSONBody in
// internal/app/agent_connect_powershell.go).
export const PS_JSON_BODY = "-ContentType 'application/json; charset=utf-8'";

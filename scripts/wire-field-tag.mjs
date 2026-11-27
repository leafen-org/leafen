// The Go tag option a contract's optional field takes, so an answer leaves
// it out when it holds nothing, the same whichever JSON package writes it:
// PocketBase writes answers with encoding/json/v2, whose omitempty leaves out
// only what writes as "", [], {} or null — a 0 or a false stays (2026-11-22:
// an ordinary page's read said authorityRevision 0, its lock databaseRow
// false) — while encoding/json, which writes MCP answers and what is stored,
// leaves those out too. A number, a bool, a pointer or an any is left out
// when it is Go's zero (omitzero: nil, not a pointer to "" or {}); a string,
// a list or a map when it is empty (omitempty, as both packages do).
//
// A field marked keepsZero means something at 0 or false that the app reads
// as such (a property's public order 0, a read's canEdit false, a page with
// hasContent false): it keeps omitempty, so answers go on writing it.
export function wireOptionalTag(field) {
  if (!field.optional) return ''
  if (field.keepsZero) return ',omitempty'
  return /^(\*|any$|interface|bool$|u?int(8|16|32|64)?$|float(32|64)$)/u.test(field.goType) ? ',omitzero' : ',omitempty'
}

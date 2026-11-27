/**
 * A text file's characters as its writer saved them, the one way every import
 * reads a text file (Markdown, plain text, CSV and the like, HTML, OPML):
 * UTF-8 or UTF-16 with its mark, else Korean CP949 when it reads as Korean,
 * else Windows-1252, so no character turns into "�". Notion reads what isn't UTF-8 as Windows-1252,
 * which shows a Korean CP949 file as "ÇÑ±Û" (seen 2026-11-05); Leafen reads it
 * as Korean first.
 */
function decodeImportText(bytes: Uint8Array): string {
  if (bytes[0] === 0xff && bytes[1] === 0xfe) return new TextDecoder("utf-16le").decode(bytes);
  if (bytes[0] === 0xfe && bytes[1] === 0xff) return new TextDecoder("utf-16be").decode(bytes);
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    // Not UTF-8; Korean is tried next.
  }
  try {
    const korean = new TextDecoder("euc-kr", { fatal: true }).decode(bytes);
    if (mostlyHangul(korean)) return korean;
  } catch {
    // Not CP949 either.
  }
  return new TextDecoder("windows-1252").decode(bytes);
}

/**
 * Whether text read as CP949 is Korean: most of what isn't ASCII is Hangul.
 * Accented Latin letters side by side can read as CP949 too, as Hanja
 * ("éè" as one character, seen 2026-11-06), and stay Windows-1252.
 */
function mostlyHangul(text: string) {
  let wide = 0, hangul = 0;
  for (const character of text) {
    const code = character.codePointAt(0)!;
    if (code < 0x80) continue;
    wide += 1;
    if ((code >= 0xac00 && code <= 0xd7a3) || (code >= 0x3131 && code <= 0x318e)) hangul += 1;
  }
  return hangul * 2 >= wide;
}

export async function readImportText(file: Pick<Blob, "arrayBuffer">) {
  return decodeImportText(new Uint8Array(await file.arrayBuffer()));
}

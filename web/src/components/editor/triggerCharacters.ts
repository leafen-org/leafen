import i18next from "i18next";

/**
 * Whether a character typed opens the "/" menu, as Notion's opener reads it
 * (its editor code, read 2026-11-18, and seen the same day in Chrome at 1440
 * by an input method's one-piece commit): "/" and the full-width "／" — and
 * "；" too with the app in Japanese, as Notion's ja-JP opener takes it. These
 * keys also leave keyboard mode as it was (keyboardMode).
 */
export function isSlashCharacter(char: string | undefined) {
  return char === "/" || char === "／" || (char === "；" && !!i18next.language?.startsWith("ja"));
}

/**
 * The trigger a character typed stands for, or null: "/" as above; "@" and
 * "+" by their compatibility form, so a full-width "＠" or "＋" opens their
 * menus as Notion's does; ":" and "[" only as themselves (a full-width "："
 * opens no emoji menu there, seen the same day).
 */
export function triggerFor(char: string | undefined): "/" | "@" | "+" | ":" | "[" | null {
  if (!char) return null;
  if (isSlashCharacter(char)) return "/";
  const plain = char.normalize("NFKD");
  if (plain === "@" || plain === "+") return plain;
  return char === ":" || char === "[" ? char : null;
}

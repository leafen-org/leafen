import { useTranslation } from "react-i18next";

/**
 * Languages whose Notion sets one's own mark against the name, with no gap:
 * Korean "이름(나)" and Japanese "名前（あなた）" (its "{name} (You)" lines in
 * every language, read 2026-11-19 from its language files of 2026-11-16, and
 * its Korean share menu seen 2026-11-19); Chinese, whose mark Leafen writes
 * with full-width brackets that carry their own room, with them.
 */
const SET_AGAINST_NAME = new Set(["ko", "ja", "zh"]);

/**
 * One's own mark after a name, "(You)" or "(나)": in every list it follows a
 * name in (the share menu and its invite field, the trash's filters, a person
 * picker), a space before it as Notion's "{name} (You)" in that language, or
 * none where Notion has none. The gap is this mark's, so a language's strings
 * carry the bracketed word alone; a mark set in a row by a flex gap of its own
 * loses the space as a line's leading space.
 */
export function SelfMark({ text, className }: { text: string; className?: string }) {
  const { i18n } = useTranslation();
  const gap = SET_AGAINST_NAME.has((i18n.language || "en").toLowerCase().split("-")[0]) ? "" : " ";
  return <span className={className} data-self-mark="">{gap}{text.trim()}</span>;
}

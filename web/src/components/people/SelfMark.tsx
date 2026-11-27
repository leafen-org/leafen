import { useTranslation } from "react-i18next";
import styles from "./SelfMark.module.css";

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
 * name in (the "@" menu's people, a person picker, the share menu and its
 * invite field, the trash's filters, the search's "created by", a teamspace's
 * members, the access preview's person) it is its own
 * span in the second text colour, a space before it as Notion's "{name} (You)"
 * in that language, or none where Notion has none (Notion's "@" menu, person
 * picker and share menu, seen 2026-11-27 in two tabs: "〈이름〉" then a grey
 * "(나)" against it). The gap and the colour are this mark's, so a language's
 * strings carry the bracketed word alone (a person picker's unless a list
 * words it its own way) and a list's own class only weighs or sizes it; a
 * mark set in a row by a flex gap of its own loses the space as a line's
 * leading space — as on a profile's title, where it follows the name's own
 * box and that box's 8px is all the room between them, as Notion's (seen
 * 2026-11-28, one's own profile, two tabs). A person card's name line is another thing: Notion writes
 * "〈이름〉 (나)" there as one string in the name's colour (its hover card,
 * seen the same day; PersonHoverCard, MentionCard by personLabel).
 */
export function SelfMark({ text, className }: { text?: string; className?: string }) {
  const { t, i18n } = useTranslation("propertyCell");
  const gap = SET_AGAINST_NAME.has((i18n.language || "en").toLowerCase().split("-")[0]) ? "" : " ";
  return <span className={className ? `${styles.mark} ${className}` : styles.mark} data-self-mark="">{gap}{(text ?? t("youMark")).trim()}</span>;
}

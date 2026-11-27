import { ExclamationCircleFillIcon } from "@/icons/leafen";
import { blockItemText } from "./blockItemLabels";
import styles from "./EmbedLoadFailedCard.module.css";

/**
 * Whether an embed's address is one its frame never loads, so the block
 * shows EmbedLoadFailedCard instead, as Notion's does (measured 2026-11-16
 * in Chrome at 1440 dark, in two tabs some 15 minutes apart, on 33 embeds
 * the block API made; one made in its link window by w1367): an address
 * that names a scheme other than https — "http://example.com/" as much as
 * "http://abc", "http://localhost/" or "http://a example.dev", "ftp://",
 * and "example.com:8080", whose "example.com:" reads as a scheme; "ws:" and
 * "javascript:" too, seen once. One with no scheme opens over https and is
 * framed whatever its host ("abc", "not a url"), as is every https one,
 * reachable or not; "mailto:" and "tel:" are framed as they are (seen
 * once). A provider's link is judged by the player it opens (an http
 * YouTube link plays).
 */
export function embedAddressRefused(address: string) {
  const scheme = /^([a-z][a-z\d+.-]*):/iu.exec(address.trim())?.[1]?.toLowerCase();
  return Boolean(scheme) && scheme !== "https" && scheme !== "mailto" && scheme !== "tel";
}

/**
 * Notion's card in an embed's frame that will not load (seen 2026-11-16 at
 * 1440 dark): the frame's whole box on the embed's placeholder ground, in
 * its middle a grey filled "!" (48px) and 14px under it a line that the
 * embed couldn't load, in 14/20 grey. Notion's "자세히 알아보기" under it opens its
 * help center; Leafen has none, as an unplayable video's plate has none.
 */
export function EmbedLoadFailedCard() {
  return <div className={styles.card} role="status" data-embed-load-failed>
    <ExclamationCircleFillIcon size="empty" className={styles.mark} aria-hidden="true" />
    <div className={styles.words}>{blockItemText("embed.loadFailed")}</div>
  </div>;
}

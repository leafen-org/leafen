import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ToggleCaretIcon } from "@/icons/leafen";
import { updateLineBoxed, updateLineWordless } from "@/lib/updateLineKinds";
import { UpdateBlockBox } from "./UpdateBlockBox";
import styles from "./UpdateLine.module.css";

const HEADINGS = new Set(["heading_1", "heading_2", "heading_3", "heading_4",
  "toggle_heading_1", "toggle_heading_2", "toggle_heading_3", "toggle_heading_4"]);

/**
 * A changed line of an update — the workspace's and the page's alike — drawn
 * by its block's kind as Notion's (seen 2026-11-17 by workers w1645 and
 * w1658, two tabs each, 1440 dark): a toggle's caret in a 14px box 6px from
 * either side and 6px down, its words 26px in; a bulleted item's bullet and a
 * numbered one's "1.", always 1, in 28px; a to-do's 16px box 3px down, 6px
 * before its words, blue with a tick when done; a heading's and a toggle
 * heading's words 600 with no mark; a code block, an equation, a quote and a
 * callout boxed under their kind (UpdateBlockBox), an added callout's box on
 * the band; a made button, breadcrumb,
 * image or synced block an empty band, a bookmark an empty slot
 * (lib/updateLineKinds.ts); a deleted button boxed under "버튼" over its name,
 * its workflow's steps under the box, all struck. The marks keep the line's
 * colour, blue on an added one's band, and a deleted line's struck with it.
 */
export function UpdateLine({ change, blockType, checked, language, was, steps, text, children }: {
  change: "added" | "changed" | "deleted";
  blockType?: string;
  /** A to-do's: whether it is done. */
  checked?: boolean;
  /** A code block's language. */
  language?: string;
  /** A changed equation's expression before the change. */
  was?: string;
  /** A deleted button's workflow's steps. */
  steps?: number;
  /** Its words as plain text, for a box that draws them so. */
  text: string;
  /** Its words as the line draws them, its marks and added words. */
  children: ReactNode;
}) {
  const { t } = useTranslation("updatesPanel");
  if (blockType === "button" && change === "deleted") return (
    <span className={styles.struck}>
      <UpdateBlockBox change={change} blockType={blockType} text={text} />
      <span className={styles.steps}>{t("inbox.activity.buttonSteps", { count: steps ?? 0 })}</span>
    </span>
  );
  if (updateLineBoxed(blockType, change)) {
    const box = (
      <UpdateBlockBox change={change} blockType={blockType ?? ""} language={language} text={text} was={was}>
        {blockType === "quote" || blockType === "callout" ? children : undefined}
      </UpdateBlockBox>
    );
    // An added callout's box stands on the added band, a quote's alone (Notion's, seen 2026-11-17, two pages).
    return blockType === "callout" && change === "added" ? <span className={styles.band}>{box}</span> : box;
  }
  if (change === "added" && updateLineWordless(blockType)) return <span className={styles.empty} />;
  switch (blockType) {
    case "toggle":
      return (
        <span className={styles.marked}>
          <span className={styles.caret}><ToggleCaretIcon className={styles.caretIcon} aria-hidden="true" /></span>
          <span className={styles.words}>{children}</span>
        </span>
      );
    case "to_do":
      return (
        <span className={styles.marked}>
          <span className={styles.check} data-checked={checked ? "" : undefined} />
          <span className={styles.words}>{children}</span>
        </span>
      );
    case "bulleted_list_item":
    case "numbered_list_item":
      // A list of its own, each a 1 (Notion's, seen 2026-11-17: an ol of one).
      return blockType === "numbered_list_item"
        ? <ol className={styles.list} data-list="numbered"><li>{children}</li></ol>
        : <ul className={styles.list} data-list="bulleted"><li>{children}</li></ul>;
  }
  return HEADINGS.has(blockType ?? "") ? <span className={styles.heading}>{children}</span> : <>{children}</>;
}

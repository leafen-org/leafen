import { Fragment, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { textDiff } from "@/lib/textDiff";
import { blockItemText } from "../editor/blockItemText";
import { codeLanguageName } from "../editor/codeLanguages";
import styles from "./UpdateBlockBox.module.css";

/**
 * A code block's, an equation's, a quote's or a callout's line in an update
 * — the workspace's and the page's alike — boxed as Notion's (seen
 * 2026-11-13, a quote's and a callout's 2026-11-17): its kind over its
 * words, "코드 - 〈language〉" over the code in its own lines, "수학 공식" over
 * the expression, "인용" and "콜아웃" over their words, a callout's box
 * standing with none; a deleted button's "버튼" over its name
 * (updates/UpdateLine); an added one on the added band, a changed equation
 * striking what went and marking what came, a changed code block its code as
 * it is now, a deleted one's words struck.
 */
export function UpdateBlockBox({ change, blockType, language, text, was, children }: {
  change: "added" | "changed" | "deleted";
  blockType: string;
  language?: string;
  /** Its words now, or as they were for a deleted one. */
  text: string;
  /** A changed equation's expression before the change. */
  was?: string;
  /** Its words as its line draws them, its marks and added words: a quote's and a callout's. */
  children?: ReactNode;
}) {
  const { t } = useTranslation("updatesPanel");
  const words = children !== undefined ? children : blockType === "equation" && change === "changed" && was !== undefined
    ? textDiff(was, text).map((part, index) => part.kind === "kept" ? <Fragment key={index}>{part.text}</Fragment>
      : <span key={index} data-added={part.kind === "added" ? "" : undefined} data-deleted={part.kind === "deleted" ? "" : undefined}>{part.text}</span>)
    : text;
  return (
    <span className={styles.box} data-change={change} data-block-type={blockType}>
      <span className={styles.kind}>{blockType === "code"
        ? t("inbox.activity.codeBlock", { language: codeLanguageName(blockItemText, language ?? "") })
        : blockType === "quote" ? t("inbox.activity.quoteBlock")
        : blockType === "callout" ? t("inbox.activity.calloutBlock")
        : blockType === "button" ? t("inbox.activity.buttonBlock") : t("inbox.activity.equationBlock")}</span>
      <span className={styles.words}>{words}</span>
    </span>
  );
}

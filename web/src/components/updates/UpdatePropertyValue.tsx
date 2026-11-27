import { ChevronRight } from "@/icons/leafen";
import type { RichTextParts } from "@/lib/richTextSummary";
import type { TextSpan } from "@/lib/types";
import { RichTextLine } from "../database/RichTextCellText";
import { HistoryValue, type PageHistoryProperty } from "../PageHistoryProperties";
import styles from "./UpdatePropertyValue.module.css";

/**
 * A database page's property value in an update, the one part the page's
 * updates tab and the workspace's updates draw it with, as Notion's both
 * draw it (seen 2026-11-13): a text by its words, those the update added
 * marked; any other as its cell shows it — an option as its 18px chip in
 * 12px type, a number in its format — after the one it replaced and a
 * faint 14px "›", 6px either side ("1234.5 › 7", "완료 › 진행 중", Notion's
 * both, seen 2026-11-14). A cleared value is its old one, which the caller
 * strikes.
 */
export function UpdatePropertyValue({ property, value, was, words, spans }: {
  property: PageHistoryProperty;
  /** The value shown, as stored: the new one, or the old one where it was cleared. */
  value: unknown;
  /** The value it replaced, as stored, for a kind other than words; none where it had none. */
  was?: unknown;
  /** A text's words split around what the update added to them, and its spans so. */
  words?: RichTextParts<string>;
  spans?: RichTextParts<TextSpan[]>;
}) {
  if (words) return (
    <>
      <RichTextLine text={words.before} spans={spans?.before} />
      {words.added ? <span data-added><RichTextLine text={words.added} spans={spans?.added} /></span> : null}
      <RichTextLine text={words.after} spans={spans?.after} />
    </>
  );
  // A box unchecked held nothing to replace.
  const replaced = !(was === null || was === undefined || was === "" || was === false || (Array.isArray(was) && was.length === 0));
  return (
    <span className={styles.value}>
      {replaced ? <><HistoryValue property={property} value={was} /><ChevronRight size="tiny" aria-hidden="true" /></> : null}
      <HistoryValue property={property} value={value} />
    </span>
  );
}

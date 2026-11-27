import { useTranslation } from "react-i18next";
import { CommentIcon, Copy, Pencil } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { useOptionalProductToast } from "@/lib/productToast";
import styles from "./PropertyValueTools.module.css";

/**
 * Which values take which tools under the pointer, as Notion's table cells
 * and page property lines (seen 2026-11-03): "댓글" on a value a person
 * set and on a formula's — though a formula's name menu offers none
 * (canCommentOnProperty) — not a title, a rollup, an ID, a checkbox, a
 * button, nor who or when made or changed the page; "클립보드에 복사" on a
 * value written out as text — not a choice, a person, a relation, a file
 * or a checkbox; and first, on a rollup's, "롤업 구성", which opens its
 * small form (seen 2026-11-06).
 */
export const COMMENTLESS_PROPERTY_TYPES: ReadonlySet<string> = new Set(["title", "rollup", "unique_id", "checkbox", "button",
  "created_time", "created_by", "last_edited_time", "last_edited_by"]);
export const COPYABLE_PROPERTY_TYPES: ReadonlySet<string> = new Set(["rich_text", "number", "date", "url", "email", "phone",
  "unique_id", "formula", "rollup", "place", "created_time", "last_edited_time"]);

/**
 * A value's tools in a white tray: "댓글" then "클립보드에 복사", which
 * copies the value as it shows (a URL as stored) and says "속성 복사 완료".
 * `line` places it on a page's property line rather than in a table cell;
 * `start` on the left, over a number's room.
 */
export function PropertyValueTools({ place = "cell", side, onConfigure, onComment, copyValue }: {
  place?: "cell" | "line";
  side?: "start";
  /** A rollup's "롤업 구성". */
  onConfigure?: (button: HTMLElement) => void;
  onComment?: (button: HTMLElement) => void;
  copyValue?: (button: HTMLElement) => string;
}) {
  const { t } = useTranslation(["propertyCell", "databaseView"]);
  const toast = useOptionalProductToast();
  if (!onConfigure && !onComment && !copyValue) return null;
  return (
    <span className={styles.tools} data-place={place} data-side={side} data-cell-value-tools>
      {onConfigure ? (
        <button type="button" className={styles.tool} data-property-configure
          aria-label={t("propertyCell:configureRollup")} title={t("propertyCell:configureRollup")}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => { event.stopPropagation(); onConfigure(event.currentTarget); }}>
          <Pencil size="small" aria-hidden="true" />
        </button>
      ) : null}
      {onComment ? (
        <button type="button" className={styles.tool} data-cell-comment
          aria-label={t("databaseView:comments")} title={t("databaseView:comments")}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={(event) => { event.stopPropagation(); onComment(event.currentTarget); }}>
          <CommentIcon size="small" aria-hidden="true" />
        </button>
      ) : null}
      {copyValue ? (
        <button type="button" className={styles.tool} data-property-copy
          aria-label={t("propertyCell:copyValue")} title={t("propertyCell:copyValue")}
          onPointerDown={(event) => event.stopPropagation()}
          onClick={async (event) => {
            event.stopPropagation();
            if (await copyText(copyValue(event.currentTarget).trim())) toast?.notify(t("propertyCell:valueCopied"));
          }}>
          <Copy size="small" aria-hidden="true" />
        </button>
      ) : null}
    </span>
  );
}

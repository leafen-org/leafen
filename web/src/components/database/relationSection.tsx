import { createContext, type MutableRefObject, type ReactNode, type RefObject, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowUpRightIcon, FileText, Plus, Search } from "@/icons/leafen";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** Whether a relation is drawn as a page section: its pages one to a line,
 *  then "↗ 기존 항목 연결" and "+ 새 페이지 추가하기" (Notion's, seen 2026-11-06). */
export const RelationSectionContext = createContext(false);

/** A page a section's "새 페이지 추가하기" made at the press, left as its title line is (PropertyRelationCell). */
export type RelationNewPage = { finish(title: string, keepUnnamed: boolean): void };

/**
 * A page section's foot, as Notion's (seen 2026-11-06): "↗ 기존 항목 연결"
 * opens the pages to choose from, "+ 새 페이지 추가하기" makes a page there
 * and then, related here, and its line is its title to type (seen
 * 2026-11-14): Enter or Escape leave it named as typed, or take it back
 * unnamed; a press elsewhere leaves it untitled.
 */
export function RelationSectionFoot({ triggerRef, open, onLink, onCreate, name, properties, startFrom }: {
  triggerRef: RefObject<HTMLButtonElement | null>;
  open: boolean;
  onLink(): void;
  onCreate?: () => RelationNewPage;
  name: string;
  /** "⋯": which of the related pages' properties the lines show. */
  properties?: ReactNode;
  /** Set to what "새 페이지 추가하기" does, for the "+" each page line brings out (RelationSectionLineAdd). */
  startFrom?: MutableRefObject<(() => void) | null>;
}) {
  const { t } = useTranslation("propertyCell");
  const [adding, setAdding] = useState(false);
  // The page being named, left once: its line goes as it is left, whatever event follows.
  const made = useRef<RelationNewPage | null>(null);
  const start = () => {
    if (!onCreate) return;
    made.current?.finish("", true);
    made.current = onCreate();
    setAdding(true);
  };
  useLayoutEffect(() => {
    if (!startFrom) return;
    startFrom.current = onCreate ? start : null;
    return () => { startFrom.current = null; };
  });
  const leave = (title: string, keepUnnamed: boolean) => {
    const page = made.current;
    made.current = null;
    setAdding(false);
    page?.finish(title.trim(), keepUnnamed);
  };
  return <>
    {adding ? (
      <span data-relation-section-new>
        <FileText size="small" aria-hidden="true" />
        <input autoFocus placeholder={t("newPageTitle")} aria-label={t("newPageTitle")}
          onKeyDown={(event) => {
            if (fieldEnter(event)) { event.preventDefault(); leave(event.currentTarget.value, false); }
            if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); leave(event.currentTarget.value, false); }
          }}
          onBlur={(event) => leave(event.currentTarget.value, true)} />
      </span>
    ) : null}
    {/* Over the section's name on hover, "+" makes a new page, 🔍 chooses pages and "⋯" the properties its
        lines show, as Notion's (seen 2026-11-06, 2026-11-07). */}
    <span data-relation-section-head-actions>
      {onCreate ? <button type="button" aria-label={t("addNewPage")} title={t("addNewPage")} onClick={(event) => { event.stopPropagation(); start(); }}>
        <Plus size="small" aria-hidden="true" /></button> : null}
      <button type="button" aria-label={t("linkExisting")} title={t("linkExisting")} onClick={(event) => { event.stopPropagation(); onLink(); }}>
        <Search size="small" aria-hidden="true" /></button>
      {properties}
    </span>
    <span data-relation-section-foot>
      <button ref={triggerRef} type="button" data-relation-section-action aria-haspopup="dialog" aria-expanded={open}
        aria-label={t("editRelation", { name })} onClick={onLink}>
        <ArrowUpRightIcon size="small" aria-hidden="true" />{t("linkExisting")}
      </button>
      {onCreate ? (
        <button type="button" data-relation-section-action onClick={(event) => { event.stopPropagation(); start(); }}>
          <Plus size="small" aria-hidden="true" />{t("addNewPage")}
        </button>
      ) : null}
    </span>
  </>;
}

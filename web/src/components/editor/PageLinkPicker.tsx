import { useEffect, useMemo, useRef, useState } from "react";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PlaceLine } from "../PlaceLine";
import { usePageNames } from "@/lib/pageNames";
import type { Page } from "@/types";
import type { BlockPickerSnapshot, RowPageMentionCandidate } from "./blockPickerModel";
import { blockItemText } from "./blockItemLabels";
import { type PageLinkChoice, pageLinkChoices } from "./pageLinkChoices";
import styles from "./editorStyles.generated.module.css";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import { usePagePathsAbove, usePathsAboveShown } from "@/lib/usePagePathsAbove";
import { outOfTrashIn, pagesByIdOf } from "@/lib/pageIndex";
import { archivedPageIds } from "@/lib/pageArchive";
import { PickerPageTitle } from "./PickerArchivedPage";

const NO_ROWS: RowPageMentionCandidate[] = [];

/**
 * Picks what a link block names (pageLinkChoices): before a search the
 * pages at the workspace's top; with one, its pages, the databases they
 * hold and the database rows the row search finds, by how well they answer.
 * No row makes a new page, and a search that
 * finds none says "결과 없음" under its head, as Notion's (seen 2026-11-19 in
 * Korean and English, a query matching some pages and one matching none). The
 * block's line is its field and the list hangs 4px under it, or over it where
 * there's no room, as Notion's (seen 2026-11-05). A page's row is one line,
 * or two with its place under its title (`data-placed`), as every Notion page
 * picker draws it (EditorPageLinks.module.css). Escape is the block's call;
 * a press outside closes the list.
 */
export function PageLinkPicker({
  pages,
  workspaceId,
  snapshot,
  initialQuery,
  onChoose,
  onEscape,
  onClose,
}: {
  pages: readonly Page[];
  workspaceId: string;
  /** The "@" menu's, whose row search and databases the picker lists too. */
  snapshot?: BlockPickerSnapshot | null;
  initialQuery: string;
  onChoose: (choice: PageLinkChoice) => void;
  onEscape: () => void;
  onClose: () => void;
}) {
  const pageNames = usePageNames();
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState(initialQuery);
  const pickerRef = useRef<HTMLDivElement>(null), holderRef = useRef<HTMLDivElement>(null);
  const { zIndex } = useOverlayLayer(true, { kind: "picker", ref: holderRef, restoreFocus: false, onEscape, onOutsidePress: onClose });
  // At most 40% of the window high, as Notion's (EditorPageLinks.module.css).
  const place = usePopoverPlacement(pickerRef, (size, viewport) => placeAround(
    popoverRectOf(inputRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4, cap: viewport.height * 0.4 },
  ), [], undefined, false);
  const pagesById = pagesByIdOf(pages);
  const archived = archivedPageIds(pages);
  const pathsRead = usePagePathsAbove([]);
  const folded = query.normalize("NFC").trim().toLowerCase();
  const outOfTrash = useMemo(() => outOfTrashIn(pages), [pages]);
  // The database rows the "@" menu's row search finds for the words, a moment after they are typed.
  const [found, setFound] = useState<{ key: string; rows: RowPageMentionCandidate[] }>({ key: "", rows: [] });
  const searchRows = snapshot?.searchRowPages;
  useEffect(() => {
    if (!folded || !searchRows) return undefined;
    const controller = new AbortController();
    const timer = window.setTimeout(() => void searchRows(folded, controller.signal)
      .then((rows) => setFound({ key: folded, rows }), () => controller.signal.aborted || setFound({ key: folded, rows: [] })), 150);
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [folded, searchRows]);
  const rows = found.key === folded ? found.rows : NO_ROWS;
  const inlineDatabases = snapshot?.inlineDatabases;
  const results = useMemo(() => pageLinkChoices({ pages, workspaceId, query, title: (page) => pageNames.title(page), archived, outOfTrash,
    inlineDatabases, rows }), [archived, inlineDatabases, outOfTrash, pages, pathsRead, query, rows, workspaceId, pageNames]);
  usePathsAboveShown(results.map(({ page }) => page), pagesById);
  const choices = results.map(result => ({ id: result.id, label: result.title, choose: () => onChoose(result) }));
  const picker = useNativeAutocomplete({ items: choices, query, onQuery: setQuery, inputRef, listRef: resultsRef, popoverRef: pickerRef,
    keyOf: choice => choice.id, textOf: choice => choice.label, onPick: choice => choice.choose(), onClose: onEscape,
    label: blockItemText("pageLink.search") });
  useEffect(() => { inputRef.current?.focus(); inputRef.current?.select(); }, []);

  return (
    <div ref={holderRef} className={styles.pageLinkHolder}>
      <input {...picker.inputProps} ref={inputRef} className={styles.pageLinkField} placeholder={blockItemText("pageLink.searchPlaceholder")} />
      <PopoverLayer><div ref={pickerRef} className={styles.pageLinkPicker} style={{ ...popoverPlacementStyle(place), zIndex }} role="dialog" aria-label={blockItemText("pageLink.linkToPage")}>
        <div className={styles.pageLinkTitle}>{blockItemText("pageLink.selectTitle")}</div>
        <div {...picker.listBoxProps} className={styles.pageLinkResults} ref={resultsRef}>
          {results.map(({ id, page, title, place: path, archived: isArchived }, index) => (
            <NativePickerOption key={id} itemKey={id} state={picker.state} data-page-link-index={index}
              data-placed={path.length ? "true" : undefined}>
              {({ labelProps, placeProps }) => <>
              <span className={styles.pageLinkResultIcon}><PageIconGlyph page={page} size={18} /></span>
              <span className={styles.pageLinkResultText}>
                <PickerPageTitle title={title} archived={isArchived} labelProps={labelProps} />
                {path.length ? <PlaceLine {...placeProps} className={styles.pageLinkResultPlace} place={path} /> : null}
              </span>
              </>}</NativePickerOption>
          ))}
          {choices.length === 0 ? <div className={styles.pageLinkEmpty}>{blockItemText("pageLink.noPages")}</div> : null}
        </div>
      </div></PopoverLayer>
    </div>
  );
}

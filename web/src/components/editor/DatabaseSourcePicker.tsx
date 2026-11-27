"use client";
import { pagesByIdOf } from "@/lib/pageIndex";

import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { placeAround, placeInWindow, popoverBackdropStyle, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { DatabaseContainerSource } from "@/lib/databaseContainerPicker";
import { Plus } from "../icons";
import { DataSourceMark } from "../database/DataSourceMark";
import { blockItemText } from "./blockItemLabels";
import type { DatabaseBlockKind } from "./blockPickerModel";
import type { BlockDef } from "./blocks";
import type { SlashMenuAnchor } from "./SlashMenu";
import styles from "./editorStyles.generated.module.css";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { foundDatabaseSourcePlace, placeText } from "@/lib/pagePath";
import { useProductPages } from "@/components/product/productPagesContext";
import { dataSourceDisplayTitle } from "./databaseTitles";
import { focusOpenedField } from "./openedFieldCaret";

const DATABASE_SOURCE_MENU_WIDTH = 360;
const DATABASE_SOURCE_MENU_GAP = 24;

export function DatabaseSourcePicker({
  anchor,
  type,
  onCreate,
  onLink,
  onClose,
  loadSources,
}: {
  anchor?: SlashMenuAnchor;
  type: DatabaseBlockKind;
  viewType?: BlockDef["databaseView"];
  onCreate: () => void;
  onLink: (source: DatabaseContainerSource) => void;
  onClose: () => void;
  loadSources: (query: string, signal: AbortSignal) => Promise<DatabaseContainerSource[]>;
}) {
  const [query, setQuery] = useState("");
  const [sources, setSources] = useState<DatabaseContainerSource[]>([]);
  const inputRef = useRef<HTMLInputElement>(null);
  const resultsRef = useRef<HTMLDivElement>(null);
  const createRef = useRef<HTMLButtonElement>(null);
  const [ownedOptions, setOwnedOptions] = useState("");
  const q = query.trim().toLowerCase();
  const placementLabel = blockItemText(
    type === "inline_database" ? "database.inlinePlacement" : "database.fullPagePlacement",
  );
  // Each source by its name and where it sits, an untitled one and the
  // untitled page holding it by their kinds' names, as Notion's (seen 2026-11-14:
  // "새 데이터베이스" over "새 페이지").
  const pages = useProductPages()?.pages;
  const results = useMemo(() => {
    const pagesById = pagesByIdOf(pages ?? []);
    return sources.map((source) => ({
      source,
      title: dataSourceDisplayTitle(source.title, source.sourceTitles?.length ?? 1),
      description: placeText(foundDatabaseSourcePlace(source, pagesById)) || blockItemText("database.label"),
    }));
  }, [pages, sources]);
  // Under the line it was asked from, or over it where only that side has
  // room; asked from nowhere in particular, in the window's middle.
  const pickerRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(pickerRef, (size, viewport) => anchor
    ? placeAround(popoverRectAt(anchor), size, viewport, { gap: DATABASE_SOURCE_MENU_GAP, minHeight: 96 })
    : placeInWindow({ left: (viewport.width - size.width) / 2, top: (viewport.height - size.height) / 2 }, size, viewport),
  [anchor], undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose });
  const menuStyle: React.CSSProperties = {
    position: "fixed",
    width: DATABASE_SOURCE_MENU_WIDTH,
    ...popoverPlacementStyle(place),
  };

  useEffect(() => focusOpenedField(() => inputRef.current, { scroll: false }), []);

  useEffect(() => {
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void loadSources(q, controller.signal).then(next => {
        if (!controller.signal.aborted) setSources(next);
      }).catch(() => {
        if (!controller.signal.aborted) setSources([]);
      });
    }, q ? 120 : 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [loadSources, q]);

  const choices: Array<{ id: string; label: string; source?: DatabaseContainerSource }> = [
    { id: "__new", label: blockItemText("databaseSource.newDatabase") },
    ...results.map(result => ({ id: result.source.id, label: result.title, source: result.source })),
  ];
  const collection = useNativeAutocomplete({ items: choices, query, onQuery: setQuery, inputRef, listRef: resultsRef, popoverRef: pickerRef,
    keyOf: item => item.id, textOf: item => item.label, onPick: item => item.source ? onLink(item.source) : onCreate(),
    onClose, label: blockItemText("databaseSource.searchExisting"),
    // Opened, and searched, nothing is lit until the arrows light it, ↓ the
    // create option first, past either end the field's step with nothing lit;
    // Enter with nothing lit makes nothing, as Notion's (seen 2026-11-14).
    highlightFirst: false, wrap: "throughNone", onEnterNone: (event) => event.preventDefault() });
  // Notion places the create option above the search. Preserve that layout
  // and declare its list ownership using the engine's actual option IDs.
  useLayoutEffect(() => {
    const ids = [createRef.current?.id, ...Array.from(resultsRef.current?.querySelectorAll<HTMLElement>('[role="option"]') ?? [], item => item.id)]
      .filter(Boolean).join(" ");
    setOwnedOptions(previous => previous === ids ? previous : ids);
  });

  const picker = (
    <>
      <button
        type="button"
        className={`${styles.menuBackdrop} ${styles.editorFloatingBackdrop}`}
        style={popoverBackdropStyle(place)}
        onClick={onClose}
        tabIndex={-1}
        aria-label={blockItemText("databaseSource.closePicker")}
      />
      <div
        ref={pickerRef}
        className={styles.databaseSourceMenu}
        style={menuStyle}
        role="dialog"
        aria-label={blockItemText("databaseSource.choose")}
        contentEditable={false}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <NativePickerOption state={collection.state} itemKey="__new" buttonRef={createRef}
          className={styles.databaseSourceItem}
          data-database-source-action="new"
        >
          {({ labelProps, descriptionProps }) => <>
          <span className={styles.databaseSourceIcon} aria-hidden="true"><Plus size="regular" /></span>
          <span className={styles.databaseSourceText}>
            <span {...labelProps} className={styles.databaseSourceTitle}>
              {blockItemText("databaseSource.newDatabase")}
            </span>
            <span {...descriptionProps} className={styles.databaseSourcePath}>
              {blockItemText("databaseSource.createNew", { placement: placementLabel })}
            </span>
          </span>
          </>}
        </NativePickerOption>
        <div className={styles.databaseSourceLabel}>
          {blockItemText("databaseSource.existingSources")}
        </div>
        <input
          {...collection.inputProps}
          ref={inputRef}
          className={styles.databaseSourceSearch}
          placeholder={blockItemText("databaseSource.searchPlaceholder")}
        />
        <div
          {...collection.listBoxProps}
          ref={resultsRef}
          className={styles.databaseSourceResults}
          aria-owns={ownedOptions || undefined}
        >
          {results.length === 0 ? (
            <div className={styles.databaseSourceEmpty}>
              {blockItemText(q ? "databaseSource.noMatching" : "databaseSource.noneYet")}
            </div>
          ) : results.map((result) => {
            return (
              <NativePickerOption state={collection.state} itemKey={result.source.id}
                key={result.source.id}
                className={styles.databaseSourceItem}
                data-database-source-action="existing"
                data-database-source-kind="database"
              >
                {({ labelProps, descriptionProps }) => <>
                <span
                  className={styles.databaseSourceIcon}
                  data-database-source-icon="database"
                  aria-hidden="true"
                ><DataSourceMark source={result.source} /></span>
                <span className={styles.databaseSourceText}>
                  <span {...labelProps} className={styles.databaseSourceTitle}>{result.title}</span>
                  <span {...descriptionProps} className={styles.databaseSourcePath}>{result.description}</span>
                </span>
                </>}
              </NativePickerOption>
            );
          })}
        </div>
      </div>
    </>
  );
  return typeof document === "undefined" ? picker : createPortal(picker, document.body);
}

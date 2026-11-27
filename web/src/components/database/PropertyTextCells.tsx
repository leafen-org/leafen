import { pageIconSource } from "@/lib/pageNames";
import { useContext, useEffect, useRef, useState, useLayoutEffect } from "react";
import { useCellText } from "./cellText";
import type { DbProperty, Page, TextSpan } from "@/lib/types";
import { hasExplicitPageIcon, PageIconGlyph } from "../page/PageIcon";
import type { PropertyCellPresentation, TitleIconMode } from "./propertyCellTypes";
import { cardShowsPageIcon } from "./CardTitleIcon";
import { ArchivedRowMark } from "./DatabaseArchivedRowsMode";
import { shortUrlParts, useUrlDisplay } from "./urlDisplay";
import archivedStyles from "./DatabaseArchivedRows.module.css";
import { CellCommentsContext } from "./cellCommentsContext";
import { propertyLink } from "./propertyLinks";
import { RichTextCellEditor } from "./RichTextCellEditor";
import { RichTextCellShown } from "./RichTextCellShown";
import { rowCellSpans } from "./richTextCellModel";
import { readOnlyStop, useCellReadOnly } from "./cellReadOnly";
import styles from "./databaseSheet";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";


export function PropertyTextCell({
  row,
  prop,
  value,
  autoFocus,
  onInput,
  presentation = "default",
  titleIcon = "own",
}: {
  row: Page;
  prop: DbProperty;
  value: unknown;
  autoFocus?: boolean;
  onInput: (value: string, spans?: TextSpan[]) => void;
  presentation?: PropertyCellPresentation;
  titleIcon?: TitleIconMode;
}) {
  const t = useCellText();
  const readOnly = useCellReadOnly();
  const cellComments = useContext(CellCommentsContext);
  const pending = cellComments?.pending;
  const initial = value == null ? "" : String(value);
  const [text, setText] = useState(initial);
  const [editing, setEditing] = useState(!!autoFocus);
  // A table's title or text sleeps until it is edited; a row page's values and a value opened to edit are awake.
  const [awake, setAwake] = useState(Boolean(autoFocus) || presentation !== "default");
  const inputRef = useRef<HTMLInputElement>(null);
  const isLink = prop.type === "url" || prop.type === "email" || prop.type === "phone";
  const urlDisplay = useUrlDisplay(prop);
  const isRowDetailText = presentation === "rowDetail" && prop.type === "rich_text";
  const iconSource = pageIconSource(row);
  // A title's or a text's words are its editor's own (RichTextCellEditor),
  // written to the row as they are typed; only a link's or a number's field
  // below shows this cell's copy of them.
  const richText = prop.type === "title" || prop.type === "rich_text";
  // A table names a row with its page's icon; an untitled one shows it only
  // with an icon of its own or content (Notion's, seen 2026-11-03).
  const showsIcon = titleIcon === "page" ? Boolean((richText ? initial : text).trim()) || cardShowsPageIcon(row)
    : titleIcon === "own" && hasExplicitPageIcon(iconSource);
  const dirtyRef = useRef(false);
  const deferredInitialRef = useRef<string | undefined>(undefined);
  const composingRef = useRef(false);
  const trailingCompositionValueRef = useRef<string | null>(null);

  // The field's copy follows the value written elsewhere. A title or a text
  // keeps none: copied after each key it drew the cell once more after every
  // key's own drawing, and React, finding a drawing still due after each of
  // some fifty quick keys in a row, took it for a loop and threw the table
  // away (2026-11-16: a long title typed fast into a new row, NAS).
  useEffect(() => {
    if (richText) return;
    if (dirtyRef.current) {
      deferredInitialRef.current = initial;
      return;
    }
    deferredInitialRef.current = undefined;
    setText(initial);
  }, [initial, richText]);

  function handleInput(next: string) {
    dirtyRef.current = true;
    setText(next);
    if (composingRef.current) return;
    const trailingCompositionValue = trailingCompositionValueRef.current;
    if (trailingCompositionValue !== null) {
      trailingCompositionValueRef.current = null;
      if (trailingCompositionValue === next) return;
    }
    onInput(next);
  }

  function startComposition() {
    composingRef.current = true;
    trailingCompositionValueRef.current = null;
  }

  function finishComposition(next: string) {
    composingRef.current = false;
    dirtyRef.current = true;
    setText(next);
    onInput(next);
    trailingCompositionValueRef.current = next;
  }

  // Over its cell where it is, the table not scrolled to it (as the number editor).
  useLayoutEffect(() => {
    if (!editing && !autoFocus) return;
    inputRef.current?.focus({ preventScroll: true });
    requestAnimationFrame(() => inputRef.current?.focus({ preventScroll: true }));
  }, [autoFocus, editing]);

  function finishEditing() {
    composingRef.current = false;
    dirtyRef.current = false;
    const deferredInitial = deferredInitialRef.current;
    deferredInitialRef.current = undefined;
    if (deferredInitial !== undefined) setText(deferredInitial);
    setEditing(false);
  }

  if (isLink && (!editing || readOnly)) {
    const link = propertyLink(prop.type, initial);
    const edit = () => { if (readOnly) return; setText(initial); setEditing(true); };
    const editLabel = t("propertyCell:edit", { name: prop.name });
    if (!initial.trim()) {
      return (
        <div className={styles.linkCell}>
          <button type="button" className={styles.numberDisplay} aria-label={editLabel} {...readOnlyStop(readOnly)} onClick={edit}>
            <span className={styles.cellEmpty}>&nbsp;</span>
          </button>
        </div>
      );
    }
    const short = prop.type === "url" && !urlDisplay.full;
    const words = short ? (() => {
      const { host, path } = shortUrlParts(initial);
      return <>{host}{path ? <span className={styles.urlPath}>{path}</span> : null}</>;
    })() : initial;
    // As Notion's (seen 2026-11-03): the value itself is the link, in the
    // text's color over a faint line, and the rest of the box edits it; a
    // URL that holds no link is plain text (seen 2026-11-12, propertyLinks.ts).
    return (
      <div className={`${styles.numberDisplay} ${styles.linkCell}`} onClick={edit}>
        <span className={styles.linkValue} data-url-short={short ? "" : undefined} title={short && !link ? initial : undefined}>
          {link ? (
            <a className={styles.linkText} href={link.href ?? undefined} target={prop.type === "url" ? "_blank" : undefined} rel="noreferrer noopener"
              tabIndex={readOnly ? -1 : undefined}
              title={short ? initial : undefined} onClick={(event) => event.stopPropagation()}>
              {words}
            </a>
          ) : words}
        </span>
        <button type="button" className={styles.linkEdit} aria-label={editLabel} {...readOnlyStop(readOnly)}
          onClick={(event) => { event.stopPropagation(); edit(); }} />
      </div>
    );
  }

  // A title or text value keeps its formatting, links and mentions. A table's
  // shows its words until it is edited, its editor made then (RichTextCellShown).
  if (prop.type === "title" || prop.type === "rich_text") {
    const fieldClass = `${styles.cellInput} ${styles.richCellInput} ${isRowDetailText ? styles.rowTextCell : ""}`;
    const editor = !awake || readOnly ? (
      <RichTextCellShown spans={rowCellSpans(row, prop.id, value)} value={value} shiftBreaks={prop.type === "rich_text"}
        ariaLabel={t("propertyCell:edit", { name: prop.name })} className={fieldClass}
        dataAttributes={prop.type === "title" ? { "data-table-title-input": "" } : undefined} onWake={() => setAwake(true)} />
    ) : (
      <RichTextCellEditor
        lineRules
        spans={rowCellSpans(row, prop.id, value)}
        value={value}
        multiline={isRowDetailText}
        // A table's text cell takes a line with ⇧Enter, as Notion's (seen 2026-11-06); Enter still ends it,
        // a page's text property's too (seen 2026-11-07: Notion saved no line break).
        shiftBreaks={prop.type === "rich_text"}
        enterEnds={isRowDetailText}
        pastedLines={isRowDetailText ? undefined : "keep"}
        // A title cell's chosen words take no bar, a text cell's do (RichTextCellEditor's title).
        titleCell={prop.type === "title"} names={prop.type === "title" ? row.id : undefined}
        textCell={prop.type === "rich_text"}
        // A table's text cell takes the phone's strip on the keyboard, its title none, as the Notion app's (CellPhoneStrip).
        phoneStrip={prop.type === "rich_text" && presentation === "default"}
        autoFocus={autoFocus}
        // A row page's value, drawn as its page opens, has its editor made after that frame (RichTextCellEditor).
        afterPaint={presentation === "rowDetail" && !autoFocus}
        ariaLabel={t("propertyCell:edit", { name: prop.name })}
        className={fieldClass}
        pageId={row.id} propertyId={prop.id} inviteOn={presentation === "rowDetail" ? row.id : undefined}
        dataAttributes={prop.type === "title" ? { "data-table-title-input": "" }
          : isRowDetailText ? { "data-row-property-text": "true" } : undefined}
        onChange={(change) => onInput(change.value, change.spans)}
        onComment={cellComments?.canComment && prop.type === "rich_text" ? (at) => cellComments.start({ rowId: row.id, property: { id: prop.id, name: prop.name, type: prop.type, config: prop.config }, ...at }) : undefined}
        onCommentMark={cellComments ? (commentIds, rect) => cellComments.open({ rowId: row.id, property: { id: prop.id, name: prop.name, type: prop.type, config: prop.config }, commentIds, rect }) : undefined}
        commentPending={pending?.rowId === row.id && pending.propertyId === prop.id ? pending : null}
      />
    );
    return prop.type === "title" ? (
      <span className={`${styles.titleCell} ${row.archivedAt ? archivedStyles.title : ""}`} data-archived-row-title={row.archivedAt ? "" : undefined} data-search-text>
        {showsIcon && (
          <span className={styles.titleCellIcon} aria-hidden="true" data-title-icon={titleIcon}>
            <PageIconGlyph page={iconSource} size={18} fallback={titleIcon === "page" ? "file" : "none"} />
          </span>
        )}
        {editor}
        {row.archivedAt ? <ArchivedRowMark /> : null}
      </span>
    ) : editor;
  }

  return (
    <input
      ref={inputRef}
      className={styles.cellInput}
      type="text"
      aria-label={t("propertyCell:edit", { name: prop.name })}
      placeholder=""
      value={text}
      readOnly={readOnly}
      tabIndex={readOnly ? -1 : undefined}
      onChange={(event) => handleInput(event.target.value)}
      onCompositionStart={startComposition}
      onCompositionEnd={(event) => finishComposition(event.currentTarget.value)}
      onBlur={finishEditing}
      onKeyDown={(event) => {
        if (imeComposing(event)) return;
        if (fieldEnter(event) || event.key === "Escape") event.currentTarget.blur();
      }}
    />
  );
}

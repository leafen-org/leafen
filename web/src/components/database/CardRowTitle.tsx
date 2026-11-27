import { useEffect, useRef, useState, type MutableRefObject, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { Page } from "@/lib/types";
import { DotsHorizontal, Pencil, SidePeekIcon } from "@/icons/leafen";
import { titleOnOneLine } from "../page/titlePaste";
import { RowTitleText } from "./RichTextCellText";
import { ArchivedRowMark } from "./DatabaseArchivedRowsMode";
import styles from "./CardRowTitle.module.css";
import { useRowDisplayTitle } from "./untitledRowName";
import { useCardTitleFit } from "./cardTitleFit";
import { keepRightPress } from "@/lib/desktopTextMenu";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { keysLedFocus } from "@/lib/lastPress";

/** A card's pencil edits its title in place. Enter, Escape and leaving the
 * field keep the title, as Notion's board does (seen 2026-10-09); focus goes
 * back to the pencil only when the keyboard started the edit — a row made in
 * place or a pencil clicked leaves nothing chosen, as Notion's (seen
 * 2026-11-03). The caller
 * keys this surface by its view context and owns the guarded row write. An
 * unrelated refreshed row must not discard the title being typed. A row just
 * made in place opens its title for typing (editNow), as Notion's list. */
export function CardRowTitle({ row, icon, editingIcon, className, readOnly, inline = false, showTitle = true, editNow = false, editCaret, opensBeside = false, onEditStarted, onEscaped, onCommit, onMenu, onOpenSide, onEditingChange, holdRef, after }: {
  row: Page; icon?: ReactNode; className?: string; readOnly: boolean; inline?: boolean; showTitle?: boolean;
  /** A card's title being typed stands by this where its page has no icon: the empty page's (Notion's board, seen 2026-11-06). */
  editingIcon?: ReactNode;
  /** What a list row shows right after its title and before its pencil: its comment count (Notion's list, seen 2026-11-06). */
  after?: ReactNode;
  editNow?: boolean; onEditStarted?(): void; onEscaped?(): void;
  /** Where the caret goes as `editNow` starts the typing: so many letters in, past the end its end. */
  editCaret?: number;
  /** A press on the row types its title, so the pointer shows the side view button, not the pencil (Notion's list with "페이지 이름 직접 편집", seen 2026-11-15). */
  opensBeside?: boolean;
  onCommit(title: string): Promise<unknown>; onMenu?(element: HTMLElement): void;
  /** While a title is typed, the pencil's place opens it in the side view, keeping what was typed (Notion's list, seen 2026-11-05, and board, 2026-11-06). */
  onOpenSide?(): void;
  /** Whether its title is being typed, for the card's property slots (CardPropertySlots). */
  onEditingChange?(editing: boolean): void;
  /** True while one of those slots' editors is open: the title leaving the caret stays typed, and ends at a press outside its card (Notion's, seen 2026-11-06). */
  holdRef?: MutableRefObject<boolean>;
}) {
  const { t } = useTranslation("tableView");
  const [draft, setDraft] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const finished = useRef(false), fromKeyboard = useRef(false), editRef = useRef<HTMLButtonElement>(null), fieldRef = useRef<HTMLInputElement>(null);
  const typed = useRef<string | null>(null), caretAt = useRef<number | undefined>(undefined);
  typed.current = draft;
  const title = useRowDisplayTitle()(row);
  const writable = !readOnly && !row.isLocked;
  // A title with line breaks keeps them until it is changed here.
  const shown = titleOnOneLine(row.title ?? "");
  useEffect(() => {
    if (!editNow || !writable) return;
    finished.current = false;
    fromKeyboard.current = false;
    caretAt.current = editCaret;
    setDraft(shown);
    onEditStarted?.();
    // Once, when asked.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editNow]);
  const editing = draft !== null;
  // A card's title wraps where Notion's does (cardTitleFit).
  const titleRef = useRef<HTMLSpanElement>(null);
  useCardTitleFit(titleRef, !inline && !editing && Boolean(row.title), title);
  useEffect(() => { onEditingChange?.(editing); }, [editing]); // eslint-disable-line react-hooks/exhaustive-deps -- reports the change only
  // Held, it ends at a press outside its card and its slots' editors.
  useEffect(() => {
    if (!editing || !holdRef) return;
    const card = fieldRef.current?.closest("[data-row-card]");
    const press = (event: PointerEvent) => {
      // A slot's editor open takes the press itself.
      if (holdRef.current || document.activeElement === fieldRef.current || !(event.target instanceof Node) || card?.contains(event.target)) return;
      holdRef.current = false;
      if (typed.current !== null) void finish(typed.current);
    };
    document.addEventListener("pointerdown", press, true);
    return () => document.removeEventListener("pointerdown", press, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- finish reads refs and state setters
  }, [editing, holdRef]);
  async function finish(value: string, returnFocus = false) {
    if (finished.current) return;
    finished.current = true;
    setDraft(null);
    if (returnFocus && fromKeyboard.current) editRef.current?.focus();
    if (!writable || value === shown) return;
    setBusy(true);
    try { await onCommit(value); } finally { setBusy(false); }
  }
  const tools = writable || onMenu ? <span className={inline ? styles.inlineTools : styles.cardTools}
    data-row-card-tools onClick={event => event.stopPropagation()} onKeyDown={event => event.stopPropagation()}
    onPointerDown={event => event.stopPropagation()} onContextMenu={keepRightPress}>
    {writable && (draft !== null || opensBeside) && onOpenSide ? <button type="button" aria-label={t("openInSidePeek")} title={t("openInSidePeek")} data-row-title-open-side
      onMouseDown={event => event.preventDefault()} onClick={() => { if (draft === null) onOpenSide(); else void finish(draft).finally(onOpenSide); }}>
      <SidePeekIcon size="small" aria-hidden="true" /></button>
    : writable && <button ref={editRef} type="button" aria-label={t("editRowTitle", { title })}
      title={t("editTitle")} disabled={busy} data-row-title-edit onClick={event => {
        finished.current = false; fromKeyboard.current = keysLedFocus(event.currentTarget); caretAt.current = undefined; setDraft(shown);
      }}><Pencil size="small" aria-hidden="true" /></button>}
    {!inline && onMenu && <button type="button" aria-label={t("rowMenu", { title })}
      title={t("rowActions")} onClick={event => onMenu(event.currentTarget)}>
      <DotsHorizontal size="small" aria-hidden="true" /></button>}
  </span> : null;
  const openSide = onOpenSide && draft !== null ? <span className={styles.inlineOpen} data-row-card-tools onClick={event => event.stopPropagation()}
    onPointerDown={event => event.stopPropagation()}>
    <button type="button" aria-label={t("openInSidePeek")} title={t("openInSidePeek")} data-row-title-open-side
      onMouseDown={event => event.preventDefault()} onClick={() => { void finish(draft).finally(onOpenSide); }}>
      <SidePeekIcon size="small" aria-hidden="true" />
    </button>
  </span> : null;
  return <>
    {!inline && tools}
    {showTitle || draft !== null ? <span ref={titleRef} className={`${className ?? ""} ${styles.title}`} data-row-title={draft === null ? "" : undefined} data-inline-editing={inline && draft !== null ? "" : undefined}>
      {draft !== null && editingIcon && !row.icon ? editingIcon : icon}
      {/* An untitled row reads "새 페이지" in the faint placeholder color, and an empty title being typed says "이름 입력", as Notion's (seen 2026-10-11). */}
      {draft === null ? row.title ? <RowTitleText row={row} title={title} />
        : <><span className={styles.untitled} data-untitled-row>{title}</span>{row.archivedAt ? <ArchivedRowMark /> : null}</>
        : <input ref={fieldRef} autoFocus data-row-title-editor placeholder={t("titlePlaceholder")}
        className={styles.input} aria-label={t("editRowTitle", { title })} value={draft}
        onFocus={event => {
          const field = event.currentTarget, at = Math.min(caretAt.current ?? field.value.length, field.value.length);
          caretAt.current = undefined;
          field.setSelectionRange(at, at);
        }} onChange={event => setDraft(event.currentTarget.value)}
        onClick={event => event.stopPropagation()} onPointerDown={event => event.stopPropagation()}
        onContextMenu={keepRightPress} onBlur={event => { if (!holdRef?.current) void finish(event.currentTarget.value); }}
        onKeyDown={event => {
          event.stopPropagation();
          if (!imeComposing(event) && (fieldEnter(event) || event.key === "Escape")) {
            event.preventDefault();
            if (event.key === "Escape") onEscaped?.();
            void finish(event.currentTarget.value, true);
          }
        }} />}
      {inline && draft === null && after ? <span className={styles.after}>{after}</span> : null}
      {inline && (openSide ?? tools)}
    </span> : null}
  </>;
}

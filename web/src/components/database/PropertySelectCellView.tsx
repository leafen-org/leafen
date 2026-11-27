import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { FocusScope } from "react-aria";
import { useTranslation } from "react-i18next";
import { CheckboxIcon, CheckIcon, ChevronRight, FlagIcon, Trash } from "@/icons/leafen";
import { placeAround, popoverBackdropStyle, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { SelectOption } from "@/lib/types";
import { chipStyle, chosenOptionColor, COLOR_NAMES, type ColorName } from "./colors";
import { confirmAction } from "../ConfirmActionDialog";
import { i18next } from "@/i18n";
import { CellMenuPortal } from "./PropertyCellMenu";
import { PhoneSelectOptionEditor } from "./PhoneSelectOptionEditor";
import { DescribeButton } from "./describeButton";
import { useSidebarLayout } from "@/components/shell/AppShell";
import styles from "./databaseSheet";
import { focusedOpener } from "@/lib/lastPress";
import { fieldEnter, fieldEscape, imeComposing } from "@/lib/fieldKeyBoundary";
import { dropOptionNameCommas } from "./optionNameText";
import { useTypedName } from "@/lib/typedName";
import type { ContentGraph, GraphOperation } from "@/contentGraph";

/**
 * Notion's notice in place of deleting a status's default (seen 2026-11-08):
 * that the default option can't be deleted, over a lone "확인", and the
 * option stays.
 */
export function tellDefaultOptionKept() {
  return confirmAction({
    message: i18next.t("propertyTypeConfig:defaultOptionKept"),
    confirmLabel: i18next.t("propertyTypeConfig:defaultOptionKeptOk"),
    cancelLabel: null,
  });
}

/**
 * Whether an option is to be deleted, asked first as Notion's (seen
 * 2026-11-06: 삭제 over 취소); a status's
 * default is told it stays (2026-11-08).
 */
async function optionDeleteConfirmed(isDefault: boolean) {
  if (isDefault) {
    await tellDefaultOptionKept();
    return false;
  }
  return confirmAction({ message: i18next.t("propertyTypeConfig:deleteOptionConfirm"), confirmLabel: i18next.t("propertyTypeConfig:deleteOption"), danger: true });
}

type OptionEditorProps = Parameters<typeof OptionEditorPopover>[0];

/**
 * An option's editor, as Notion's (seen 2026-10-05): its name with an "i"
 * that opens a line for its description, "삭제", and "색" with each color
 * named and the chosen one checked; on a phone a whole sheet
 * (PhoneSelectOptionEditor).
 */
export function PropertySelectOptionEditor(props: OptionEditorProps) {
  const { phone } = useSidebarLayout(true);
  if (!phone) return <OptionEditorPopover {...props} />;
  const { onDelete, isDefault = false, onClose } = props;
  return <PhoneSelectOptionEditor {...props} onDelete={onDelete ? async () => {
    const confirmed = await optionDeleteConfirmed(isDefault);
    if (confirmed) onDelete();
    if (confirmed || isDefault) onClose();
  } : undefined} />;
}

function OptionEditorPopover({
  option,
  anchor,
  onClose,
  onRename,
  typedChange,
  onColor,
  onDescribe,
  onDelete,
  isDefault = false,
  onMakeDefault,
  group,
  align = "end",
}: {
  option: SelectOption;
  anchor: DOMRect;
  onClose: () => void;
  onRename: (name: string) => void;
  /** The change of its property saving a name typed makes, on the page's graph as drawn: kept on the device at each key (useTypedName). */
  typedChange?: (name: string, graph: ContentGraph) => GraphOperation[] | null;
  onColor: (color: ColorName) => void;
  onDescribe?: (description: string) => void;
  onDelete?: () => void;
  /** Its status's default, whose "삭제" tells it stays (tellDefaultOptionKept). */
  isDefault?: boolean;
  /** A status option that isn't its default yet, as Notion's "기본으로 설정". */
  onMakeDefault?: () => void;
  /** A status option's group, moved to another from "그룹화 [묶음] ›", as Notion's (seen 2026-11-02). */
  group?: OptionGroupChoice;
  /** Lined up with the option's row end (its settings), or from the start of the "⋯" that opened it (a cell's picker), slid into the window. */
  align?: "start" | "end";
}) {
  const { t } = useTranslation(["propertyCell", "propertyTypeConfig", "common"]);
  // Right under the option, lined up with its right edge or from the start
  // of its "⋯", or over it where only that side has room (Notion's, seen
  // 2026-11-06: under a cell picker's "⋯" at 1197, 1048..1268 in a 1280 window;
  // 2026-11-08: 42..461 over a "⋯" at 461). Shown whole: with room on neither
  // side, under it moved up into the window (2026-11-08: 292..711 under a row at 419..447).
  const editorRef = useRef<HTMLDivElement>(null);
  // Its description shows once "ⓘ" is pressed, written or not, as Notion's (seen 2026-11-06).
  const [describing, setDescribing] = useState(false);
  // Closing hands focus back to what opened the editor, as a menu does; with
  // that gone (the option deleted), to the menu row whose submenu held it, as
  // Notion's gives it to "속성 편집" (seen 2026-11-06).
  const [opener] = useState(() => focusedOpener());
  const [openerRow] = useState(() => document.querySelector<HTMLElement>('[role="menuitem"][aria-expanded="true"]'));
  const deleted = useRef(false);
  // Closed by Escape, the focus goes to the side window holding its option,
  // which holds it with no ring, so the next Escape closes that window: Notion's
  // leaves no ring on the option (seen 2026-11-14).
  const keyClosed = useRef(false);
  const closeByKey = () => { keyClosed.current = true; onClose(); };
  // The name takes the focus, its words chosen, once the editor's own focus
  // scope is set: taken as it is drawn, a cell picker's scope took it back.
  const nameRef = useRef<HTMLInputElement>(null);
  useEffect(() => { nameRef.current?.focus({ preventScroll: true }); }, []);
  // Its name is kept however it closes — Enter, a press outside, Escape, the page left — as Notion's option editor
  // keeps it (seen 2026-11-28 in Chrome, two tabs: a name typed, then Escape, stayed, from 속성 편집 and from a cell's "⋯").
  const named = useTypedName(option.id, "option", onRename, typedChange);
  useEffect(() => {
    const editor = editorRef.current;
    return () => {
      const active = document.activeElement;
      if (active && active !== document.body && !editor?.contains(active)) return;
      const holder = keyClosed.current ? opener?.closest<HTMLElement>("[data-submenu]") : null;
      if (holder?.isConnected && !deleted.current) holder.focus({ preventScroll: true });
      else if (opener?.isConnected && !deleted.current) opener.focus({ preventScroll: true });
      else openerRow?.focus({ preventScroll: true });
    };
  }, [opener, openerRow]);
  const place = usePopoverPlacement(
    editorRef, (size, viewport) => placeAround(anchor, size, viewport, { gap: 0, align, whole: true }), [anchor, align], undefined, { kind: "inner" },
  );
  return createPortal(
    <>
      <button
        type="button"
        className={styles.menuBackdrop}
        tabIndex={-1}
        aria-label={t("propertyCell:closeOptionEditor")}
        style={popoverBackdropStyle(place) ?? { zIndex: 1002 }}
        onClick={onClose}
      />
      {/* Its own focus scope, so a picker it opens from keeps none of its focus. */}
      <FocusScope>
      <div
        className={styles.optionEditor}
        role="dialog"
        aria-label={t("propertyCell:edit", { name: option.name })}
        ref={editorRef}
        data-option-editor={option.id}
        style={{ position: "fixed", right: "auto", zIndex: 1003, ...popoverPlacementStyle(place) }}
        // Escape is the editor's own: it closes it from the name, a color or
        // a row, one step, and does nothing in the description, never the
        // menus it opened from (Notion's, seen 2026-11-06 and 2026-11-14: a
        // color pressed, Escape closed the editor, then the menus one each).
        onKeyDown={(event) => {
          if (event.key !== "Escape") return;
          event.stopPropagation();
          if (event.defaultPrevented || imeComposing(event) || event.target instanceof HTMLTextAreaElement) return;
          event.preventDefault();
          closeByKey();
        }}
      >
        <span className={styles.optionRenameBox}>
          <input
            className={styles.optionRename}
            defaultValue={option.name}
            aria-label={t("propertyCell:optionName")}
            ref={nameRef}
            onFocus={(event) => event.currentTarget.select()}
            onInput={(event) => { dropOptionNameCommas(event.currentTarget); named.typed(event.currentTarget.value); }}
            onKeyDown={(event) => {
              if (imeComposing(event)) return;
              if (fieldEnter(event)) {
                event.preventDefault();
                named.saved(event.currentTarget.value);
                onClose();
              } else if (event.key === "Escape") {
                event.preventDefault();
                closeByKey();
              }
            }}
            onBlur={(event) => named.saved(event.target.value)}
          />
          {onDescribe && (
            <DescribeButton className={styles.optionDescribe}
              name={t("propertyTypeConfig:addOptionDescription")} open={describing} data-option-describe
              onClick={() => setDescribing(true)} />
          )}
        </span>
        {onDescribe && describing && (
          <textarea className={styles.optionDescription} defaultValue={option.description ?? ""} rows={1} maxLength={2000}
            aria-label={t("propertyTypeConfig:optionDescription")} placeholder={t("propertyTypeConfig:optionDescriptionPlaceholder")}
            autoFocus data-option-description
            onFocus={(event) => { const end = event.currentTarget.value.length; event.currentTarget.setSelectionRange(end, end); }}
            onBlur={(event) => onDescribe(event.target.value.trim())}
            onKeyDown={(event) => {
              if (!fieldEnter(event) || event.shiftKey) return;
              event.preventDefault();
              onDescribe(event.currentTarget.value.trim());
            }} />
        )}
        {onDelete && (
          // Asked first (optionDeleteConfirmed); a status's default stays and the editor closes.
          <button type="button" className={styles.optionEditorRow} data-option-delete onClick={async () => {
            if (!await optionDeleteConfirmed(isDefault)) {
              if (isDefault) onClose();
              return;
            }
            deleted.current = true;
            onDelete();
            onClose();
          }}>
            <Trash size="regular" aria-hidden="true" /><span>{t("propertyTypeConfig:deleteOption")}</span>
          </button>
        )}
        {onMakeDefault && (
          <button type="button" className={styles.optionEditorRow} data-option-make-default onClick={() => { onMakeDefault(); onClose(); }}>
            <FlagIcon size="regular" aria-hidden="true" /><span>{t("propertyTypeConfig:makeDefaultOption")}</span>
          </button>
        )}
        {group ? <OptionGroupRow group={group} /> : null}
        <div className={styles.optionEditorHeading}>{t("propertyTypeConfig:optionColorHeading")}</div>
        <div className={styles.optionColors} role="group" aria-label={t("propertyTypeConfig:optionColorHeading")}>
          {COLOR_NAMES.map((color) => (
            <button
              key={color}
              type="button"
              className={styles.optionEditorRow}
              data-active={chosenOptionColor(option) === color ? "true" : undefined}
              data-option-color={color}
              aria-pressed={chosenOptionColor(option) === color}
              onClick={() => onColor(color)}
            >
              <span className={styles.optionColorChip} style={chipStyle(color)} aria-hidden="true" />
              <span>{t(`propertyTypeConfig:optionColors.${color}`)}</span>
              {chosenOptionColor(option) === color && <CheckIcon size="small" aria-hidden="true" />}
            </button>
          ))}
        </div>
      </div>
      </FocusScope>
    </>,
    document.body,
  );
}


export type OptionGroupChoice = { current: string; choices: { id: string; label: string }[]; onMove(groupId: string): void };

function OptionGroupRow({ group }: { group: OptionGroupChoice }) {
  const { t } = useTranslation("propertyTypeConfig");
  const [open, setOpen] = useState(false);
  const row = useRef<HTMLButtonElement>(null);
  const current = group.choices.find((choice) => choice.id === group.current)?.label ?? "";
  return <>
    <button ref={row} type="button" className={styles.optionEditorRow} aria-haspopup="menu" aria-expanded={open} data-option-group
      onClick={() => setOpen((shown) => !shown)}>
      <CheckboxIcon size="regular" aria-hidden="true" />
      <span>{t("optionGroup")}</span><span className={styles.optionEditorMeta}>{current}</span><ChevronRight size="small" aria-hidden="true" />
    </button>
    {open ? <CellMenuPortal beside triggerRef={row} onClose={() => setOpen(false)} ariaLabel={t("optionGroup")} width={200} className={styles.optionEditor}
      // Escape closes it alone, back on its row: the editor holding it keeps
      // Escape from the menus under it (OptionEditorPopover).
      onKeyDown={(event) => {
        if (!fieldEscape(event)) return;
        event.preventDefault(); event.stopPropagation();
        setOpen(false); row.current?.focus({ preventScroll: true });
      }}>
      {group.choices.map((choice) => <button key={choice.id} type="button" className={styles.optionEditorRow} aria-pressed={choice.id === group.current}
        data-option-group-choice={choice.id} onClick={() => { group.onMove(choice.id); setOpen(false); }}>
        <span>{choice.label}</span>{choice.id === group.current ? <CheckIcon size="small" aria-hidden="true" /> : null}
      </button>)}
    </CellMenuPortal> : null}
  </>;
}

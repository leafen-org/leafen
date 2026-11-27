import { useMemo, useRef, useState, type FormEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { ContentGraph } from "@/contentGraph";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { EnterSign } from "@/components/EnterSign";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { useTypedName } from "@/lib/typedName";
import type { SelectOption } from "@/lib/types";
import { dropOptionNameCommas, optionNameWithoutCommas, optionRenameChange, optionsRenamed, otherOptionNames } from "./optionNameText";
import styles from "./databaseSheet";

// The name field's size, as its stylesheet's, to keep it inside the window.
const GROUP_NAME_WIDTH = 300;
const GROUP_NAME_HEIGHT = 36;

/**
 * The small window a group is named in — "+ 신규 그룹"'s new name
 * (NewGroupName) and a group name chip's rename (GroupRename), one window in
 * Notion's table, list, gallery and board alike (seen 2026-11-28 in Chrome
 * at 1440, dpr 1: 300 by 36, the field 14px, "완료 ↵" 24 high, 14px/500,
 * 6px in from each side, 6 between its word and its sign) — `gap` under what
 * opened it, its left on that one's, or over it where it would come within
 * 12px of the window's foot. A name another group has is refused with
 * "그룹이 이미 존재합니다." and the name stays to change, as Notion's says it
 * (seen 2026-10-30 adding, 2026-11-28 renaming a select's group twice);
 * answered, the focus goes back where it was when the notice opened (the
 * notice's own return): the field after Enter in it, "완료 ↵" after a press of
 * it. `done` takes the name and says whether the focus goes back to what
 * opened the window (true) or is left on the page; `leave` is Escape — from
 * the field alone where `escapeFromField` — or a press outside, the focus
 * going back.
 */
function GroupNameBox({ anchor, gap, initial = "", placeholder, label, taken, escapeFromField = false, returnTo, onInput, done, leave }: {
  anchor: DOMRect; gap: number; initial?: string; placeholder: string; label: string; taken: readonly string[];
  escapeFromField?: boolean; returnTo?: HTMLElement; onInput?(field: HTMLInputElement): void;
  done(name: string): boolean; leave(): void;
}) {
  const { t } = useTranslation("tableView");
  const [name, setName] = useState(initial);
  const [refusing, setRefusing] = useState(false);
  const boxRef = useRef<HTMLFormElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const opener = useRef(returnTo ?? null);
  const returns = useRef(true);
  const { zIndex } = useOverlayLayer(true, {
    kind: "picker", ref: boxRef, initialFocus: inputRef, returnFocusRef: opener, shouldRestoreFocus: () => returns.current,
    onEscape: refusing ? undefined : () => { if (!escapeFromField || document.activeElement === inputRef.current) leave(); },
    onOutsidePress: refusing ? undefined : leave,
  });
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const value = name.trim();
    if (!value || !taken.includes(value)) {
      returns.current = done(value);
      if (!returns.current) inputRef.current?.blur();
      return;
    }
    setRefusing(true);
    void confirmAction({ message: t("groupExists"), confirmLabel: t("groupExistsOk"), cancelLabel: null })
      .then(() => setRefusing(false));
  };
  const spot = { top: anchor.bottom + gap + GROUP_NAME_HEIGHT > window.innerHeight - POPOVER_MARGIN ? anchor.top - gap - GROUP_NAME_HEIGHT : anchor.bottom + gap,
    left: Math.max(POPOVER_MARGIN, Math.min(anchor.left, window.innerWidth - GROUP_NAME_WIDTH - POPOVER_MARGIN)) };
  return (
    <form ref={boxRef} className={styles.newGroupName} data-table-new-group-name={initial ? undefined : ""} data-group-rename={initial ? "" : undefined}
      style={{ zIndex, ...spot }} onSubmit={submit}>
      <input ref={inputRef} value={name} placeholder={placeholder} aria-label={label}
        onFocus={initial ? (event) => event.currentTarget.select() : undefined}
        onChange={(event) => { onInput?.(event.currentTarget); setName(event.currentTarget.value); }} />
      <button type="submit">{t("newGroupDone")} <EnterSign /></button>
    </form>
  );
}

/**
 * The new group's name, asked under the button that adds it, its left on the
 * button's (GroupNameBox): measured 2026-11-16 in Chrome at 1440 in two tabs
 * 7 minutes apart with no script in the page before the action — after
 * "확인", a typed "z" went into the field after Enter and nowhere after the
 * press, the focus on "완료". Added or left with Escape, it gives the focus
 * back to what held it as it opened (ViewGroupAdd).
 */
export function NewGroupName({ anchor, taken = [], onDone }: {
  anchor: DOMRect; taken?: readonly string[]; onDone(name: string | null): void;
}) {
  const { t } = useTranslation("tableView");
  return <GroupNameBox anchor={anchor} gap={0} placeholder={t("newGroupName")} label={t("newGroupName")} taken={taken}
    done={(name) => { onDone(name || null); return true; }} leave={() => onDone(null)} />;
}

/**
 * A group's name renamed in place from its chip in the group's head — a
 * board's column or sub-group, a table's, list's, gallery's or timeline's
 * group — as Notion's (seen 2026-11-28 in Chrome at 1440 in two tabs at two
 * times, no script in the page before the action, a letter typed after it):
 * the window 4px under the chip's 3px-padded press area, its left on it, the
 * name in it chosen; Enter or "완료 ↵" renames the option (the property's
 * options written as `onRename` writes them), the focus left on the page; an
 * empty name keeps the old one. Escape from the field and a press outside
 * let the words go, the focus back on the chip, ringed after Escape; Tab
 * goes to "완료 ↵", where Escape does nothing. A select's, multi-select's or
 * status's option alike; a status renamed to another of its options' names
 * is refused here as a select's is (Notion's merges the two). While it is
 * open the words are kept on this device at each key as the change saving
 * them makes, and saved should the page go first (useTypedName).
 */
export function GroupRename({ property, optionId, name, returnTo, onRename, onClose }: {
  /** The grouping property as drawn: its id and settings, its options in them. */
  property: { id: string; config?: unknown };
  optionId: string;
  name: string;
  /** The chip pressed: where the window opens and the focus goes back. */
  returnTo: HTMLElement;
  /** Writes the property's options, the one renamed. */
  onRename(options: SelectOption[]): void;
  onClose(): void;
}) {
  const { t } = useTranslation("boardView");
  const [anchor] = useState(() => returnTo.getBoundingClientRect());
  const taken = useMemo(() => otherOptionNames(property.config, optionId), [optionId, property.config]);
  // A name another group has is never kept nor saved: it is refused as it is pressed.
  const renamed = (typed: string) => taken.includes(optionNameWithoutCommas(typed).trim()) ? null : optionsRenamed(property.config, optionId, typed);
  const named = useTypedName(optionId, "option", (typed) => { const next = renamed(typed); if (next) onRename(next); },
    (typed: string, graph: ContentGraph) => {
      const next = renamed(typed);
      return optionRenameChange(graph, property.id, next && { ...(property.config as object | undefined), options: next });
    });
  return createPortal(
    <GroupNameBox anchor={anchor} gap={4} initial={name} placeholder={t("renameGroup")} label={t("groupName")} taken={taken}
      escapeFromField returnTo={returnTo}
      onInput={(field) => { dropOptionNameCommas(field); named.typed(field.value); }}
      done={(typed) => {
        const next = renamed(typed);
        if (next) named.saved(typed); else named.settled();
        onClose();
        return !next;
      }}
      leave={() => { named.settled(); onClose(); }} />,
    document.body,
  );
}

import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { Fragment, useContext, useEffect, useId, useRef, useState, type ReactNode, type RefObject } from "react";
import { TouchSheetLayout, useSidebarLayout } from "@/components/shell/AppShell";
import { PhoneSheet, PhoneSheetGroup } from "../shell/PhoneSheet";
import { newId } from "@/lib/ids";
import { useDragReorder } from "@/lib/useDragReorder";
import type { DbProperty, Page, SelectOption } from "@/lib/types";
import { DotsHorizontal, DragHandleIcon, SlidersIcon, XMarkIcon } from "../icons";
import { chipStyle, type ColorName, newOptionColor, recoloredOption } from "./colors";
import { statusGroupForOption, statusGroupLabelKey, synchronizeStatusGroupMembership, type statusGroupColumns } from "./propertyStatusPolicy";
import { CellMenuPortal, CellPickerHostContext } from "./PropertyCellMenu";
import { PropertySelectOptionEditor } from "./PropertySelectCellView";
import { PropertyStatusSheet } from "./PropertyStatusSheet";
import { optionSortOf, sortedOptions } from "./selectOptionSort";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { useOptionDescriptionTip } from "./OptionDescriptionTip";
import { menuIconButtonProps } from "./menuIconButton";
import styles from "./databaseSheet";
import { useDatabaseStructureLocked } from "./databaseStructureLock";
import { useDatabasePropertySettings } from "./databasePropertySettings";
import { NativePickerOption, useNativeAutocomplete } from "@/lib/nativeAutocomplete";
import { useCellText } from "./cellText";
import { optionNameWithoutCommas } from "./optionNameText";

/**
 * A select's, a multi-select's or a status's open picker (PropertySelectCell):
 * its search, its options to choose, make, order, recolor and rename, and a
 * status's "속성 편집". Closing it unmounts it, so it opens on an empty search.
 * On a touch screen it is Notion's sheet named for the property (seen
 * 2026-11-14 on an iPad at 834): the chosen options in a group of their own,
 * "옵션 선택 또는 생성" over the options, each with its "⋯", and no keyboard
 * until the field is pressed; a status's has its groups alone (PropertyStatusSheet).
 */
export function PropertySelectPicker({
  row,
  prop,
  multi,
  operations,
  context,
  options,
  statusGroups,
  selectedIds,
  selected,
  triggerRef,
  newColor,
  setNewColor,
  closeMenu,
}: {
  row: Page;
  prop: DbProperty;
  multi: boolean;
  options: SelectOption[];
  statusGroups: ReturnType<typeof statusGroupColumns>;
  selectedIds: string[];
  selected: SelectOption[];
  triggerRef: RefObject<HTMLButtonElement | null>;
  newColor: ColorName;
  setNewColor(color: ColorName): void;
  closeMenu(restoreFocus?: boolean): void;
} & Pick<PropertyCellEnvironment, "operations" | "context">) {
  const t = useCellText();
  // A new option changes the property, which a locked database keeps as it is.
  const structureLocked = useDatabaseStructureLocked();
  // Renaming, recoloring and ordering options change the property itself.
  const optionsFixed = structureLocked || operations.addOptionsOnly === true;
  // A status's picker ends in "속성 편집", which opens its settings in the view's, as Notion's (seen 2026-10-10).
  const openPropertySettings = useDatabasePropertySettings();
  const editProperty = prop.type === "status" && !optionsFixed && openPropertySettings
    ? () => { closeMenu(); openPropertySettings(prop.id); } : null;
  const [q, setQ] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const { touch } = useSidebarLayout(true);
  const ariaLabel = multi ? t("propertyCell:editMultiSelectProperty") : t("propertyCell:editSelectProperty");
  const [colorPickerFor, setColorPickerFor] = useState<string | null>(null);
  const optionTip = useOptionDescriptionTip();
  const [colorPickerRect, setColorPickerRect] = useState<DOMRect | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  // A status always has one value, chosen from its groups (Notion, seen 2026-10-08).
  const status = prop.type === "status";

  function commit(ids: string[]) {
    void operations.setValue(row.id, prop, multi ? ids : (ids[0] ?? null), context);
  }
  function toggle(id: string) {
    if (multi) {
      const next = selectedIds.includes(id)
        ? selectedIds.filter((x) => x !== id)
        : [...selectedIds, id];
      commit(next);
    } else {
      // A select's option chosen again stays and nothing is written, its
      // last edit kept; a status's is written again, its last edit moving (a
      // default the row only shows is set); neither adds to the page's
      // updates, as Notion's (seen 2026-11-11).
      if (status || String(row.properties?.[prop.id] ?? "") !== id) commit([id]);
      closeMenu(true);
    }
  }
  // Choosing an option in a multi-select adds it and clears what was typed,
  // so the next words search again; one already chosen stays, taken out only
  // by its "×" or Backspace, as Notion's (seen 2026-11-07: "M1" Enter then
  // "M2" made a new option "M1M2", and Enter on a chosen option took it out).
  function choose(id: string) {
    if (!multi) {
      toggle(id);
      return;
    }
    if (!selectedIds.includes(id)) commit([...selectedIds, id]);
    setQ("");
  }
  // A chosen option taken out with its "×" (or Backspace) leaves the picker
  // open on its field, as Notion's (2026-11-15, worker Chrome, no script
  // before the press: the "×" of a multi-select's first option, then a letter
  // went into the picker's field, in both).
  function removeChosen(id: string) {
    commit(selectedIds.filter((x) => x !== id));
    inputRef.current?.focus({ preventScroll: true });
  }
  function createOption(name: string) {
    const group = statusGroups[0];
    const opt: SelectOption = {
      id: newId(),
      name,
      color: newColor,
      ...(group ? { group: group.name } : {}),
    };
    updateOptions(sortedOptions([...options, opt], optionSortOf(prop.config)));
    toggle(opt.id);
    setQ("");
    setNewColor(newOptionColor());
  }
  function updateOptions(next: SelectOption[], patch: Record<string, unknown> = {}) {
    void operations.updateProperty(prop.id, {
      config:
        prop.type === "status"
          ? synchronizeStatusGroupMembership(prop.config, next)
          : { ...prop.config, ...patch, options: next } as DbProperty["config"],
    }, context);
  }
  function reorderOption(sourceId: string, targetId: string, side: "before" | "after") {
    if (!sourceId || !targetId || sourceId === targetId) return;
    const source = options.find((option) => option.id === sourceId);
    if (!source || !options.some((option) => option.id === targetId)) return;
    const next = options.filter((option) => option.id !== sourceId);
    const targetIndex = next.findIndex((option) => option.id === targetId);
    if (targetIndex < 0) return;
    const target = next[targetIndex];
    const targetGroup = target ? statusGroupForOption(prop.config, target) : undefined;
    next.splice(
      targetIndex + (side === "after" ? 1 : 0),
      0,
      // One with no color chosen takes its new group's.
      targetGroup ? { ...source, group: targetGroup.name, ...(source.colorFromGroup ? { color: targetGroup.color } : {}) } : source
    );
    updateOptions(next, { optionSort: undefined });
  }
  // Options drag within this picker only.
  const pickerId = useId();
  const optionReorder = useDragReorder(`select-option:${pickerId}`, reorderOption, { live: true });
  // The option's menu stays open on its new color, as Notion's (seen 2026-11-06).
  function setOptionColor(id: string, color: ColorName) {
    updateOptions(options.map((o) => (o.id === id ? recoloredOption(o, color) : o)));
  }
  function renameOption(id: string, name: string) {
    const trimmed = name.trim();
    if (!trimmed) return;
    updateOptions(sortedOptions(options.map((o) => (o.id === id ? { ...o, name: trimmed } : o)), optionSortOf(prop.config)));
  }
  function describeOption(id: string, description: string) {
    if ((options.find((o) => o.id === id)?.description ?? "") === description) return;
    updateOptions(options.map((o) => (o.id === id ? { ...o, description: description || undefined } : o)));
  }
  // "삭제" takes the option away; a status keeps at least one.
  function deleteOption(id: string) {
    if (prop.type === "status" && options.length <= 1) return;
    updateOptions(options.filter((o) => o.id !== id));
  }
  // What's typed finds an option by its words, or by them without their
  // commas, the name it would make (optionNameText.ts).
  const filtered = options.filter((o) =>
    [q, optionNameWithoutCommas(q)].some((typed) => o.name.toLowerCase().includes(typed.toLowerCase()))
  );
  const firstVisibleStatusOptionIds = new Set(
    statusGroups.flatMap((group) => {
      const option = filtered.find(
        (candidate) => statusGroupForOption(prop.config, candidate)?.id === group.id
      );
      return option ? [option.id] : [];
    })
  );
  // What's typed makes an option named without its commas, as Notion's
  // offers it (optionNameText.ts); the field keeps what was typed.
  const newName = optionNameWithoutCommas(q).trim();
  const exact = options.some((o) => [q.trim().toLowerCase(), newName.toLowerCase()].includes(o.name.toLowerCase()));
  // A status's options are added in its settings only, its picker making none
  // and drawing no option "⋯", as Notion's (seen 2026-11-07).
  const canCreate = !structureLocked && !status && newName.length > 0 && !exact;
  const choices = [
    ...filtered.map(option => ({ id: option.id, label: option.name, choose: () => choose(option.id) })),
    ...(canCreate ? [{ id: "__create", label: `${t("propertyCell:createOption")} ${newName}`, choose: () => createOption(newName) }] : []),
    ...(editProperty ? [{ id: "__edit_property", label: t("propertyCell:editProperty"), choose: editProperty }] : []),
  ];
  // It opens with no row marked; what's typed marks the first, as Notion's
  // (seen 2026-11-02, 11-03); the arrows ring the row they mark, the pointer
  // grounding the one under it apart (seen 2026-11-14). Measured once
  // 2026-11-15 in Chrome 1440 with no script before the press: opened by a
  // press, no row marked; "alpha" typed marked "생성 alpha", Enter picked it,
  // closing on the cell below chosen; a press on the field's empty end left
  // the keys in the search.
  const picker = useNativeAutocomplete({ items: choices, query: q, onQuery: setQ, keyOf: item => item.id,
    textOf: item => item.label, onPick: item => item.choose(), onClose: () => closeMenu(true), highlightFirst: q.trim().length > 0,
    inputRef, listRef, popoverRef, open: true, label: t("propertyCell:searchOptionsAria") });

  const field = (
        <>
        {/* The chosen options stand in the field before what's typed, as
            Notion's (seen 2026-10-08); a select's go with ×, a status's stays. */}
        {/* eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions */}
        <div className={styles.selectField} onClick={() => inputRef.current?.focus()}>
          {selected.map((o) => (
            <span key={o.id} className={styles.chip} style={chipStyle(o.color)} data-status-chip={status ? "" : undefined}
              data-selected-option={o.id}>
              <span className={styles.chipText}>{o.name}</span>
              {status ? null : (
                <button type="button" className={styles.chipRemove} aria-label={t("propertyCell:removeOption", { name: o.name })}
                  onClick={(event) => { event.stopPropagation(); removeChosen(o.id); }}><XMarkIcon size="small" aria-hidden="true" /></button>
              )}
            </span>
          ))}
          <input {...picker.inputProps} ref={inputRef} className={styles.selectFieldInput} autoFocus={!touch}
            placeholder={selected.length ? undefined : t("propertyCell:searchOptionsAria")}
            onKeyDown={(event) => {
              // Backspace in an empty field takes the last chosen option back.
              if (event.key === "Backspace" && !q && !status && selected.length > 0) {
                event.preventDefault();
                removeChosen(selected[selected.length - 1].id);
                return;
              }
              picker.inputProps.onKeyDown?.(event);
            }} />
        </div>
        </>
  );
  const list = (
        <div {...picker.listBoxProps} className={styles.selectList} ref={listRef}
          aria-multiselectable={multi ? "true" : undefined} data-status-list={status || undefined}>
          {filtered.map((o, index) => {
            const statusGroup = statusGroupForOption(prop.config, o);
            const row = optionReorder.row(o.id, () => true, !optionsFixed);
            return (
            <Fragment key={o.id}>
              {statusGroup && firstVisibleStatusOptionIds.has(o.id) && (
                <div className={styles.statusGroupLabel} role="presentation">
                  {statusGroupLabelKey(statusGroup.name) ? t(`propertyCell:${statusGroupLabelKey(statusGroup.name)}`) : statusGroup.name}
                </div>
              )}
            <div
              className={styles.selectOpt}
              data-active={picker.activeKey === o.id ? "true" : undefined}
              data-option-id={o.id}
              onMouseEnter={(event) => optionTip.show(event, o)}
              onMouseLeave={optionTip.hide}
              onMouseDown={optionTip.hide}
              {...row}
            >
              {!optionsFixed && !status && !touch && (
              // No tip, as Notion's handle has none (seen 2026-11-18); its name is for readers.
              <span
                className={styles.selectOptionDragHandle}
                aria-label={t("propertyCell:dragOption", { name: o.name })}
                data-select-option-drag-handle={o.id}
                // Pressed, the handle holds focus itself: the picker
                // would otherwise pull focus back and the browser would
                // not start the drag.
                tabIndex={-1}
                {...optionReorder.handle(o.id, true, { above: filtered[index - 1]?.id, below: filtered[index + 1]?.id })}
              >
                <DragHandleIcon size="small" aria-hidden="true" />
              </span>
              )}
              <NativePickerOption itemKey={o.id} state={picker.state} className={styles.selectOptMain}
                aria-selected={selectedIds.includes(o.id)} data-select-index={index}>
                {({ labelProps }) => <>
                <span {...labelProps} className={styles.chip} style={chipStyle(o.color)} data-status-chip={status ? "" : undefined}>
                  <span className={styles.chipText}>{o.name}</span>
                </span>
                </>}</NativePickerOption>
              {!optionsFixed && !status && (
              <button
                type="button"
                className={styles.optionColorBtn}
                {...menuIconButtonProps(t("propertyCell:edit", { name: o.name }), colorPickerFor === o.id)}
                onClick={(e) => {
                  const rect = popoverAnchorOf(e.currentTarget);
                  setColorPickerFor((cur) => (cur === o.id ? null : o.id));
                  setColorPickerRect(rect);
                }}
              >
                <DotsHorizontal size="small" aria-hidden="true" />
              </button>
              )}
            </div>
            </Fragment>
            );
          })}
          {canCreate && (
            <NativePickerOption itemKey="__create" state={picker.state} className={styles.selectCreate}
              aria-selected={false} data-select-index={filtered.length}>{({ labelProps }) => <span {...labelProps} className={styles.selectCreateLabel}>
              <span>{t("propertyCell:createOption")}</span>
              <span className={styles.chip} style={chipStyle(newColor)}>
                <span className={styles.chipText}>{newName}</span>
              </span>
            </span>}</NativePickerOption>
          )}
          {editProperty && <>
            <div className={styles.selectEditDivider} role="separator" />
            <NativePickerOption itemKey="__edit_property" state={picker.state} className={styles.selectCreate}
              aria-selected={false} data-select-edit-property>{({ labelProps }) => <>
              <SlidersIcon size="regular" aria-hidden="true" /><span {...labelProps}>{t("propertyCell:editProperty")}</span></>}</NativePickerOption>
          </>}
        </div>
  );
  const after = (
        <>
        {optionTip.tip}
        {/* Drawn within the picker, whose focus it may then take. */}
        {colorPickerFor && colorPickerRect && (() => {
          const option = options.find((candidate) => candidate.id === colorPickerFor);
          return option ? (
            <PropertySelectOptionEditor
              option={option}
              anchor={colorPickerRect}
              align="start"
              // Closed, it gives the focus to the picker's field, as Notion's (2026-11-15, Chrome, no script
              // before the action; judged by the screen and one typed letter: an option's "…", Escape — the
              // letter went into the picker's field).
              onClose={() => { inputRef.current?.focus({ preventScroll: true }); setColorPickerFor(null); }}
              onRename={(name) => renameOption(option.id, name)}
              onColor={(color) => setOptionColor(option.id, color)}
              onDescribe={(description) => describeOption(option.id, description)}
              onDelete={prop.type === "status" && options.length <= 1 ? undefined : () => deleteOption(option.id)}
            />
          ) : null;
        })()}
        </>
  );
  // A database its own lock holds makes no option here: its heading asks only to choose, and says so in its place when
  // nothing matches, as Notion's (seen 2026-11-18 in Chrome, two tabs: a locked inline database's row page read "옵션
  // 선택", and "옵션을 찾지 못했습니다" for a name it lacks, with no "생성" row).
  const heading = status ? null : !structureLocked ? t("propertyCell:selectOrCreateOption")
    : q.trim() && filtered.length === 0 ? t("propertyCell:noOptionsFound") : t("propertyCell:selectOption");
  if (touch && status) {
    return <PropertyStatusSheet prop={prop} options={options} groups={statusGroups} label={ariaLabel} sheetRef={popoverRef}
      onChoose={toggle} onEditProperty={editProperty ?? undefined} onClose={() => closeMenu(true)} after={<PickerOpened />} />;
  }
  if (touch) {
    return (
      <TouchSheetLayout>
        <PhoneSheet size="half" title={prop.name} label={ariaLabel} sheetRef={popoverRef} onClose={() => closeMenu(true)} data={{ "data-select-sheet": "" }}>
          <PhoneSheetGroup>{field}</PhoneSheetGroup>
          <PhoneSheetGroup label={heading ?? undefined}>{list}</PhoneSheetGroup>
          {after}
        </PhoneSheet>
        <PickerOpened />
      </TouchSheetLayout>
    );
  }
  return (
    <CellMenuPortal
      popoverRef={popoverRef}
      triggerRef={triggerRef}
      onClose={() => closeMenu(true)}
      ariaLabel={ariaLabel}
      className={`${styles.selectMenu} ${status ? styles.statusPicker : styles.selectPicker}`}
      backdropClassName={styles.menuBackdrop}
      // A select's and a multi-select's 300, a status's 240, as Notion's (seen 2026-11-03).
      width={status ? 240 : 300}
      scroll={false}
      cover
      tabLeavesCell
    >
      {field}
      {heading ? <div className={styles.propMenuLabel}>{heading}</div> : null}
      {list}
      {after}
    </CellMenuPortal>
  );
}

/** Tells a table's selection that a picker is open, as CellMenuPortal does. */
export function PickerOpened(): ReactNode {
  const host = useContext(CellPickerHostContext);
  useEffect(() => host?.opened(), [host]);
  return null;
}

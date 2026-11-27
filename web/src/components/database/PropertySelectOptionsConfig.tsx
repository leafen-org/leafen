"use client";

import { Fragment, useEffect, useId, useRef, useState } from "react";
import { popoverAnchorOf } from "@/lib/popoverPlacement";
import { Trans, useTranslation } from "react-i18next";
import { newId } from "@/lib/ids";
import { statusDefaultOptionId } from "@/lib/statusDefaultValue";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { useDragReorder } from "@/lib/useDragReorder";
import type { DbProperty, SelectOption } from "@/lib/types";
import { ChevronRight, DragHandleIcon, Plus } from "../icons";
import { chipStyle, newOptionColor, recoloredOption, type ColorName } from "./colors";
import { SortIcon } from "../icons";
import { NotionSelect } from "./NotionSelect";
import { PhonePropertySection, usePhonePropertySettings } from "./PhonePropertySettings";
import phoneStyles from "./PhonePropertySettings.module.css";
import { PropertySelectOptionEditor } from "./PropertySelectCellView";
import { dropOptionNameCommas, optionRenameChange } from "./optionNameText";
import { OPTION_SORTS, optionSortOf, sortedOptions, type OptionSort } from "./selectOptionSort";
import {
  statusGroupColumns,
  statusGroupForOption,
  statusGroupLabelKey,
  statusOptionsInGroupOrder,
  synchronizeStatusGroupMembership,
} from "./propertyStatusPolicy";
import type {
  PropertyTypeConfigOperationContext,
  PropertyTypeConfigOps,
} from "./propertyTypeConfigTypes";
import styles from "./databaseSheet";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * A select's, multi-select's or status's options in its settings, as
 * Notion's (seen 2026-10-05): "정렬" (by hand, alphabetical or reverse) for a
 * select, then "옵션" with its "+", each option with a handle to drag it,
 * its chip and ›, which opens its editor (name, description, "삭제", colors);
 * a status lists its options under their groups, each with its own "+" and no
 * "옵션" over them, its chips round with their dots (seen 2026-11-02).
 * In a phone's "속성 편집" "정렬" is a row of the type's group and each
 * "옵션 +" a section under it (PhonePropertySection), as Notion's there.
 */
export function PropertySelectOptionsConfig({
  operationContext,
  ops,
  prop,
}: {
  operationContext: PropertyTypeConfigOperationContext;
  ops: PropertyTypeConfigOps;
  prop: DbProperty;
}) {
  const { t } = useTranslation(["propertyTypeConfig", "common", "propertyCell"]);
  // Where a new option is being named: "options", or a status's group.
  const [adding, setAdding] = useState<string | null>(null);
  const [newOptionName, setNewOptionName] = useState("");
  const [editing, setEditing] = useState<{ id: string; anchor: DOMRect } | null>(null);
  // Done naming a new option, focus goes back to its "+" rather than the page.
  const plusRef = useRef<HTMLButtonElement | null>(null);
  const wasAdding = useRef(false);
  useEffect(() => {
    if (wasAdding.current && adding === null && (!document.activeElement || document.activeElement === document.body)) plusRef.current?.focus();
    wasAdding.current = adding !== null;
  }, [adding]);

  const statusGroups = prop.type === "status" ? statusGroupColumns(prop.config) : [];
  const options =
    statusGroups.length > 0
      ? statusOptionsInGroupOrder(prop.config)
      : prop.config?.options ?? [];
  const sort = optionSortOf(prop.config);
  // A status's default, which its rows read while they set none, shows "기본" as Notion's.
  const defaultOptionId = prop.type === "status" ? statusDefaultOptionId((prop.config ?? {}) as Record<string, unknown>) : null;

  // A new default switches the rows that set none, so Notion asks first when
  // there was one (seen 2026-11-08: "계속" in red over "취소"), not from none.
  async function makeDefault(option: SelectOption) {
    if (defaultOptionId && !await confirmAction({
      message: t("propertyTypeConfig:changeDefaultQuestion", { name: option.name }),
      detail: <Trans t={t} i18nKey="propertyTypeConfig:changeDefaultDetail" values={{ name: option.name }} components={{ b: <b /> }} />,
      confirmLabel: t("propertyTypeConfig:changeDefaultContinue"),
      danger: "filled",
      roomy: true,
    })) return;
    void ops.updateProperty(operationContext, prop.id, {
      config: { ...prop.config, defaultOptionId: option.id } as DbProperty["config"],
    });
  }

  // The property's settings with these options, as they are saved.
  function optionsConfig(next: SelectOption[], patch: Record<string, unknown> = {}) {
    return prop.type === "status"
      ? synchronizeStatusGroupMembership({ ...prop.config, ...patch } as DbProperty["config"], next)
      : { ...prop.config, ...patch, options: next } as DbProperty["config"];
  }
  function updateOptions(next: SelectOption[], patch: Record<string, unknown> = {}) {
    void ops.updateProperty(operationContext, prop.id, { config: optionsConfig(next, patch) });
  }

  function uniqueOptionName(base: string) {
    const names = new Set(options.map((option) => option.name.trim().toLowerCase()));
    const trimmed = base.trim() || t("propertyTypeConfig:defaultOptionName");
    if (!names.has(trimmed.toLowerCase())) return trimmed;
    for (let i = 2; i < 1000; i += 1) {
      const candidate = `${trimmed} ${i}`;
      if (!names.has(candidate.toLowerCase())) return candidate;
    }
    return `${trimmed} ${newId().slice(0, 4)}`;
  }

  // A new option goes where the sort puts it, a status's at its group's top; Enter keeps the field
  // for the next, as Notion's (seen 2026-11-02), and leaving it closes it. A status's takes its
  // group's color for its own, another's any (Notion's, seen 2026-11-14: gray, blue, green).
  function createOption(keepAdding = false) {
    const group = statusGroups.find((candidate) => candidate.id === adding) ?? statusGroups[0];
    const option: SelectOption = {
      id: newId(),
      name: uniqueOptionName(newOptionName || t("propertyTypeConfig:defaultOptionName")),
      color: group?.color || newOptionColor(),
      ...(group ? { group: group.name } : {}),
    };
    const first = group ? options.findIndex((candidate) => statusGroupForOption(prop.config, candidate)?.id === group.id) : -1;
    // A status's default stays the option it was, though the new one heads its
    // first group, as Notion's (seen 2026-11-07: "시작 전" kept its "기본").
    const keepDefault = prop.type === "status" && defaultOptionId && !(prop.config as Record<string, unknown> | undefined)?.defaultOptionId
      ? { defaultOptionId } : {};
    // A new option comes first, in its group or among the options, as Notion's (seen 2026-11-06: D, C, B, A).
    updateOptions(group ? [...options.slice(0, Math.max(0, first < 0 ? options.length : first)), option,
      ...options.slice(Math.max(0, first < 0 ? options.length : first))] : sortedOptions([option, ...options], sort), keepDefault);
    setNewOptionName("");
    if (!keepAdding) setAdding(null);
  }

  // A status option moved to another group goes to its end, taking its color where it had none chosen.
  function moveOptionToGroup(id: string, groupId: string) {
    const group = statusGroups.find((candidate) => candidate.id === groupId);
    const option = options.find((candidate) => candidate.id === id);
    if (!group || !option || statusGroupForOption(prop.config, option)?.id === groupId) return;
    const moved = { ...option, group: group.name, ...(option.colorFromGroup ? { color: group.color } : {}) };
    const rest = options.filter((candidate) => candidate.id !== id);
    const last = rest.map((candidate) => statusGroupForOption(prop.config, candidate)?.id).lastIndexOf(groupId);
    rest.splice(last < 0 ? rest.length : last + 1, 0, moved);
    updateOptions(rest, { optionSort: undefined });
  }

  function changeOption(id: string, patch: Partial<SelectOption>) {
    updateOptions(options.map((option) => (option.id !== id ? option
      : patch.color === undefined ? { ...option, ...patch } : { ...recoloredOption(option, patch.color), ...patch })));
  }

  // The options with one renamed as its name is saved, or null when it stays.
  function optionsNamed(id: string, name: string) {
    const trimmed = name.trim();
    const current = options.find((option) => option.id === id);
    if (!current || trimmed === current.name) return null;
    const named = { name: trimmed || uniqueOptionName(t("propertyTypeConfig:defaultOptionName")) };
    return sortedOptions(options.map((option) => (option.id === id ? { ...option, ...named } : option)), sort);
  }
  function commitOptionName(id: string, name: string) {
    const next = optionsNamed(id, name);
    if (next) updateOptions(next);
  }

  function chooseSort(next: string) {
    const value = next as OptionSort;
    updateOptions(sortedOptions(options, value), { optionSort: value === "manual" ? undefined : value });
  }

  // Dragging an option orders the list by hand from then on.
  function reorderOption(sourceId: string, targetId: string, side: "before" | "after") {
    if (!sourceId || !targetId || sourceId === targetId) return;
    const source = options.find((option) => option.id === sourceId);
    if (!source) return;
    const next = options.filter((option) => option.id !== sourceId);
    const targetIndex = next.findIndex((option) => option.id === targetId);
    if (targetIndex < 0) return;
    const target = next[targetIndex];
    const targetGroup = target ? statusGroupForOption(prop.config, target) : undefined;
    next.splice(
      targetIndex + (side === "after" ? 1 : 0),
      0,
      // One with no color chosen takes its new group's.
      targetGroup ? { ...source, group: targetGroup.name, ...(source.colorFromGroup ? { color: targetGroup.color } : {}) } : source,
    );
    updateOptions(next, { optionSort: undefined });
  }
  // Options drag within this list only.
  const listId = useId();
  const optionReorder = useDragReorder(`select-option-config:${listId}`, reorderOption, { live: true });

  const editingOption = editing ? options.find((option) => option.id === editing.id) : undefined;
  const status = prop.type === "status";
  const phone = usePhonePropertySettings();

  function optionHead(label: string, place: string, plus = true) {
    return (
      <div className={styles.propertyOptionHead}>
        <span>{label}</span>
        {plus && <button type="button" className={styles.propertyOptionPlus} aria-label={t("propertyTypeConfig:addOption")}
          title={t("propertyTypeConfig:addOption")} data-option-add={place}
          onClick={(event) => { plusRef.current = event.currentTarget; setAdding(place); }}>
          {/* A 20px "+" filling its 20px button, as Notion's "옵션" and status group heads (seen 2026-11-17 at 1440 in two tabs). */}
          <Plus size="regular" aria-hidden="true" />
        </button>}
      </div>
    );
  }

  function optionRow(option: SelectOption) {
    const index = options.indexOf(option);
    const row = optionReorder.row(option.id);
    return (
      <div
        key={option.id}
        className={phone ? phoneStyles.optionRow : styles.propertyOptionRow}
        {...row}
        data-option-row={option.id}
      >
        <button
          type="button"
          className={phone ? phoneStyles.optionDrag : styles.propertyOptionDrag}
          aria-label={t("propertyTypeConfig:reorderOption", { name: option.name })}
          {...optionReorder.handle(option.id, true, { above: options[index - 1]?.id, below: options[index + 1]?.id })}
        >
          <DragHandleIcon size="small" aria-hidden="true" />
        </button>
        <button type="button" className={phone ? phoneStyles.optionOpen : styles.propertyOptionOpen} aria-haspopup="dialog"
          aria-expanded={editing?.id === option.id} aria-label={t("propertyTypeConfig:editOption", { name: option.name })}
          data-option-open={option.id}
          onClick={(event) => setEditing({ id: option.id, anchor: popoverAnchorOf(event.currentTarget) })}>
          <span className={styles.propertyOptionChip} style={chipStyle(option.color)} data-status-chip={status ? "" : undefined}>{option.name}</span>
          {option.id === defaultOptionId ? <span className={phone ? phoneStyles.optionDefault : styles.propertyOptionDefault}>{t("propertyTypeConfig:defaultOptionBadge")}</span> : null}
          <ChevronRight size="small" aria-hidden="true" />
        </button>
      </div>
    );
  }

  // A list's "+" on a phone, at its section's head.
  const phoneAdd = (place: string) => ({ label: t("propertyTypeConfig:addOption"), data: { "data-option-add": place },
    onPress: (button: HTMLButtonElement) => { plusRef.current = button; setAdding(place); } });

  const newOptionField = (
    <input
      className={phone ? phoneStyles.optionNew : styles.propertyOptionNew}
      value={newOptionName}
      autoFocus
      placeholder={t("propertyTypeConfig:newOptionPlaceholder")}
      aria-label={t("propertyTypeConfig:newOptionName")}
      data-option-new
      onChange={(e) => { dropOptionNameCommas(e.currentTarget); setNewOptionName(e.currentTarget.value); }}
      onBlur={() => { if (newOptionName.trim()) createOption(); else setAdding(null); }}
      onKeyDown={(e) => {
        if (imeComposing(e)) return;
        if (fieldEnter(e)) {
          e.preventDefault();
          if (newOptionName.trim()) createOption(true);
        } else if (e.key === "Escape") {
          e.preventDefault();
          e.stopPropagation();
          setNewOptionName("");
          setAdding(null);
        }
      }}
    />
  );

  const sortRow = prop.type !== "status" && (
    <div data-option-sort>
      <NotionSelect ariaLabel={t("propertyTypeConfig:optionSort")} settingLabel={t("propertyTypeConfig:optionSort")} settingIcon={<SortIcon size="small" />} value={sort}
        options={OPTION_SORTS.map((value) => ({ value, label: t(`propertyTypeConfig:optionSorts.${value}`) }))}
        onChange={chooseSort} />
    </div>
  );
  const groupLabel = (name: string) => statusGroupLabelKey(name) ? t(`propertyCell:${statusGroupLabelKey(name)}`) : name;
  const phoneLists = phone ? (statusGroups.length > 0 ? statusGroups.map((group) => (
    <PhonePropertySection key={group.id} label={groupLabel(group.name)} add={phoneAdd(group.id)} data={{ "data-status-group": group.id }}>
      {adding === group.id && newOptionField}
      {options.filter((option) => statusGroupForOption(prop.config, option)?.id === group.id).map(optionRow)}
    </PhonePropertySection>
  )) : (
    <PhonePropertySection label={t("propertyTypeConfig:options")} add={phoneAdd("options")} data={{ "data-select-option-list": "" }}>
      {adding === "options" && newOptionField}
      {options.map(optionRow)}
    </PhonePropertySection>
  )) : null;
  return (
    <div className={styles.propertyOptionConfig} data-status-options={status ? "" : undefined} data-select-options={status ? undefined : ""}>
      {sortRow}
      {phoneLists}
      {/* Sorting and the options parted by a line, as Notion's (seen 2026-11-06). */}
      {!phone && prop.type !== "status" && <div className={styles.propertyOptionRule} role="separator" />}
      {phone ? null : statusGroups.length > 0 ? statusGroups.map((group) => (
        <Fragment key={group.id}>
          {/* A standard group reads in the reader's language, as Notion's "할 일·진행 중·완료" (seen 2026-10-11). */}
          {/* Its "+" hides, every group's, while a new option is named, as Notion's (seen 2026-11-14). */}
          {optionHead(groupLabel(group.name), group.id, adding === null)}
          <div className={styles.propertyOptionList} data-status-group={group.id}>
            {adding === group.id && newOptionField}
            {options.filter((option) => statusGroupForOption(prop.config, option)?.id === group.id).map(optionRow)}
          </div>
        </Fragment>
      )) : (
        <>
          {/* As Notion's (seen 2026-11-06): with no options "옵션" over "+ 옵션 추가", with
              some its "+" at the end; a new option is named in a field over them. */}
          {optionHead(t("propertyTypeConfig:options"), "options", options.length > 0 && adding === null)}
          <div className={styles.propertyOptionList}>
            {adding === "options" && newOptionField}
            {options.map(optionRow)}
            {options.length === 0 && adding === null && (
              <button type="button" className={styles.propertyOptionAddRow} data-option-add="options"
                onClick={(event) => { plusRef.current = event.currentTarget; setAdding("options"); }}>
                <Plus size="regular" aria-hidden="true" /><span>{t("propertyTypeConfig:addOption")}</span>
              </button>
            )}
          </div>
        </>
      )}
      {editing && editingOption && (
        <PropertySelectOptionEditor
          option={editingOption}
          anchor={editing.anchor}
          onClose={() => setEditing(null)}
          onRename={(name) => commitOptionName(editingOption.id, name)}
          typedChange={(name, graph) => {
            const next = optionsNamed(editingOption.id, name);
            return optionRenameChange(graph, prop.id, next && optionsConfig(next));
          }}
          onColor={(color: ColorName) => changeOption(editingOption.id, { color })}
          onDescribe={(description) => {
            if ((editingOption.description ?? "") !== description) changeOption(editingOption.id, { description: description || undefined });
          }}
          onDelete={prop.type === "status" && options.length <= 1 ? undefined
            : () => updateOptions(options.filter((option) => option.id !== editingOption.id))}
          isDefault={prop.type === "status" && editingOption.id === defaultOptionId}
          group={status ? { current: statusGroupForOption(prop.config, editingOption)?.id ?? "",
            choices: statusGroups.map((group) => ({ id: group.id, label: statusGroupLabelKey(group.name) ? t(`propertyCell:${statusGroupLabelKey(group.name)}`) : group.name })),
            onMove: (groupId) => moveOptionToGroup(editingOption.id, groupId) } : undefined}
          // Every status option offers "기본으로 설정", the default too, where it
          // changes nothing (Notion's, seen 2026-11-08 on a status made in its screen).
          onMakeDefault={prop.type === "status"
            ? () => { if (editingOption.id !== defaultOptionId) void makeDefault(editingOption); }
            : undefined}
        />
      )}
    </div>
  );
}

import { useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { useTranslation } from "react-i18next";
import { CaretRightFill, CheckIcon, ChevronDown, Plus, Trash } from "@/icons/leafen";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import type { DbProperty, FormConditionGroup, SelectOption } from "@/lib/types";
import styles from "./FormView.module.css";
import { fieldFocusOnOpen } from "@/lib/screenKind";

function ConditionChoices({ anchor, property, group, onChange, onClose }: {
  anchor: RefObject<HTMLElement | null>;
  property: DbProperty;
  group: FormConditionGroup;
  onChange(patch: Partial<FormConditionGroup>): void;
  onClose(): void;
}) {
  const { t } = useTranslation("formView");
  const menu = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState("");
  const place = usePopoverPlacement(menu, (size, viewport) => placeAround(
    popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4 }),
  [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current] });
  if (property.type === "checkbox") {
    return (
      <PopoverLayer><div ref={menu} className={styles.conditionMenu} role="menu" aria-label={t("conditionChoose")} style={popoverPlacementStyle(place)} data-form-condition-menu>
        {([[false, "conditionUnchecked"], [true, "conditionChecked"]] as const).map(([checked, key]) => (
          <button key={key} type="button" role="menuitemradio" aria-checked={group.checked === checked} className={styles.conditionItem}
            data-form-condition-checked={checked} onClick={() => onChange({ checked })}>
            <span>{t(key)}</span>{group.checked === checked ? <CheckIcon size="small" aria-hidden="true" /> : null}
          </button>
        ))}
        {group.checked !== undefined ? (
          <>
            <div className={styles.questionMenuDivider} />
            <button type="button" role="menuitem" className={styles.conditionItem} data-form-condition-clear
              onClick={() => onChange({ checked: undefined })}>{t("clearAnswer")}</button>
          </>
        ) : null}
      </div></PopoverLayer>
    );
  }
  const options: readonly SelectOption[] = property.config?.options ?? [];
  const chosen = group.optionIds ?? [];
  const shown = options.filter((option) => option.name.toLowerCase().includes(query.trim().toLowerCase()));
  return (
    <PopoverLayer><div ref={menu} className={styles.conditionMenu} role="menu" aria-label={t("conditionChoose")} style={popoverPlacementStyle(place)} data-form-condition-menu>
      <input autoFocus={fieldFocusOnOpen()} className={styles.conditionSearch} value={query} placeholder={t("conditionChoosePlaceholder")}
        aria-label={t("conditionChoosePlaceholder")} onChange={(event) => setQuery(event.target.value)} />
      {shown.map((option) => {
        const on = chosen.includes(option.id);
        return (
          <button key={option.id} type="button" role="menuitemcheckbox" aria-checked={on} className={styles.conditionItem} data-condition-option={option.id}
            onClick={() => onChange({ optionIds: on ? chosen.filter((id) => id !== option.id) : [...chosen, option.id] })}>
            <span className={styles.conditionItemName}>
              <span className={styles.conditionCheck} data-on={on || undefined} aria-hidden="true">{on ? <CheckIcon size="tiny" /> : null}</span>
              {option.name}
            </span>
          </button>
        );
      })}
    </div></PopoverLayer>
  );
}

function ConditionMatch({ anchor, matchAll, onChange, onClose }: {
  anchor: RefObject<HTMLElement | null>; matchAll: boolean; onChange(matchAll: boolean): void; onClose(): void;
}) {
  const { t } = useTranslation("formView");
  const menu = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menu, (size, viewport) => placeAround(
    popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { gap: 4 }),
  [anchor], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current] });
  return (
    <PopoverLayer><div ref={menu} className={styles.conditionMenu} role="menu" aria-label={t("conditionOf")} style={popoverPlacementStyle(place)} data-form-condition-match-menu>
      {([[false, "conditionMatchAny"], [true, "conditionMatchAll"]] as const).map(([all, key]) => (
        <button key={key} type="button" role="menuitemradio" aria-checked={matchAll === all} className={styles.conditionItem}
          onClick={() => { onChange(all); onClose(); }}>
          <span>{t(key)}</span>{matchAll === all ? <CheckIcon size="small" aria-hidden="true" /> : null}
        </button>
      ))}
    </div></PopoverLayer>
  );
}

/**
 * A question's condition group in the builder, as Notion's (seen 2026-11-05):
 * under the question, "▼ [옵션 ⌄] 필터가 선택된 경우" and, pointing, a bin that
 * takes the group and its questions; a line beside its question cards, then
 * "다른 질문 추가" — or, empty, one wide "질문 추가 또는 선택". Folded, the row
 * counts its questions.
 */
export function FormConditionGroupView({ group, property, count, adding, children, onChange, onRemove, onAdding, addMenu }: {
  group: FormConditionGroup;
  property: DbProperty;
  count: number;
  adding: boolean;
  children: ReactNode;
  onChange(patch: Partial<FormConditionGroup>): void;
  onRemove(): void;
  onAdding(open: boolean): void;
  addMenu(anchor: RefObject<HTMLElement | null>): ReactNode;
}) {
  const { t } = useTranslation("formView");
  const [folded, setFolded] = useState(false);
  const [choosing, setChoosing] = useState(false);
  const [matching, setMatching] = useState(false);
  const chip = useRef<HTMLButtonElement>(null), matchChip = useRef<HTMLButtonElement>(null), addButton = useRef<HTMLButtonElement>(null);
  // A group just asked for comes into view with its choice of a question.
  useEffect(() => { if (adding) addButton.current?.scrollIntoView({ block: "center" }); }, [adding]);
  const checkbox = property.type === "checkbox";
  const names = (property.config?.options ?? []).filter(({ id }) => group.optionIds?.includes(id)).map(({ name }) => name);
  const value = checkbox
    ? group.checked === true ? t("conditionWhenChecked") : group.checked === false ? t("conditionWhenUnchecked") : t("conditionChoose")
    : names.length ? names.join(", ") : t("conditionChoose");
  return (
    <div className={styles.conditionGroup} data-form-condition-group={group.id} data-folded={folded || undefined}>
      <div className={styles.conditionHead}>
        <button type="button" className={styles.conditionFold} aria-expanded={!folded} aria-label={t(folded ? "conditionExpand" : "conditionFold")}
          onClick={() => setFolded((current) => !current)}>
          <CaretRightFill aria-hidden="true" />
        </button>
        <button ref={chip} type="button" className={styles.conditionChip} aria-haspopup="menu" aria-expanded={choosing} data-form-condition-chip
          onClick={() => setChoosing((open) => !open)}>
          <span>{value}</span><ChevronDown size="tiny" aria-hidden="true" />
        </button>
        {!checkbox && (group.optionIds?.length ?? 0) > 1 ? <>
          <span className={styles.conditionWords}>{t("conditionOf")}</span>
          <button ref={matchChip} type="button" className={styles.conditionChip} aria-haspopup="menu" aria-expanded={matching} data-form-condition-match
            onClick={() => setMatching((open) => !open)}>
            <span>{t(group.matchAll ? "conditionMatchAll" : "conditionMatchAny")}</span><ChevronDown size="tiny" aria-hidden="true" />
          </button>
          {matching ? <ConditionMatch anchor={matchChip} matchAll={group.matchAll === true} onChange={(matchAll) => onChange({ matchAll })} onClose={() => setMatching(false)} /> : null}
        </> : null}
        <span className={styles.conditionWords}>{t(checkbox ? "conditionFilter" : "conditionFilterChosen")}</span>
        {folded ? <span className={styles.conditionCount} aria-label={t("conditionCount", { count })}>
          <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.25" aria-hidden="true">
            <path d="M4 3v4a3 3 0 0 0 3 3h5" /><path d="M10 7.5 12.5 10 10 12.5" />
          </svg>{count}</span> : null}
        <button type="button" className={styles.conditionRemove} aria-label={t("conditionRemove")} title={t("conditionRemove")} data-form-condition-remove
          onClick={onRemove}><Trash size="small" aria-hidden="true" /></button>
      </div>
      {choosing ? <ConditionChoices anchor={chip} property={property} group={group} onChange={onChange} onClose={() => setChoosing(false)} /> : null}
      {folded ? null : (
        <div className={styles.conditionBody}>
          {count > 0 ? children : null}
          <button ref={addButton} type="button" className={count > 0 ? styles.conditionAdd : styles.conditionSlot} aria-haspopup="menu" aria-expanded={adding}
            data-form-condition-add onClick={() => onAdding(!adding)}>
            <span className={styles.conditionAddMark}><Plus size="small" aria-hidden="true" /></span>
            {t(count > 0 ? "conditionAddAnother" : "conditionAddFirst")}
          </button>
          {adding ? addMenu(addButton) : null}
        </div>
      )}
    </div>
  );
}

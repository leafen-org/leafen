import { useId, useRef, useState, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ChevronUpDown, X } from "@/icons/leafen";
import { placeAtPoint, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import styles from "./FormDropdown.module.css";
import { inlineStartX } from "@/lib/readingDirection";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

type Option = { id: string; name: string };

/**
 * A form's dropdown answer as Notion's respondents see it (seen 2026-10-21):
 * the field holds the chosen values as grey chips, or "입력한 답변", with ⇕
 * at its end. Opened, a list lies over the field: a search line holding the
 * chosen values, each with ×, then "옵션 선택" and the options in plain text,
 * the first match of a search marked for Enter. One of several is added and
 * the list stays open, a chosen one picked again staying chosen; a single one
 * replaces the value and closes. Backspace in an empty search takes the last
 * chosen value off, Escape closes back to the field.
 */
export function FormDropdown({ label, placeholder, options, values, multiple, max, disabled, onChange }: {
  label: string;
  placeholder: string;
  options: readonly Option[];
  values: readonly string[];
  multiple: boolean;
  /** At most this many of several. */
  max?: number;
  disabled: boolean;
  onChange: (values: string[]) => void;
}) {
  const { t } = useTranslation("formView");
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(-1);
  const [width, setWidth] = useState(0);
  const field = useRef<HTMLButtonElement>(null), list = useRef<HTMLDivElement>(null), search = useRef<HTMLInputElement>(null);
  const listId = useId();
  const byId = new Map(options.map((option) => [option.id, option]));
  // A chosen value no longer an option keeps its chip, as a removed option's id.
  const chosen = values.map((id) => byId.get(id) ?? { id, name: id });
  const shown = options.filter((option) => option.name.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));

  function close(back = true) {
    setOpen(false);
    setQuery("");
    setActive(-1);
    if (!back) return;
    field.current?.focus();
    // A pick by pointer loses it as the list goes; it comes back once the list is gone.
    window.requestAnimationFrame(() => { if (document.activeElement === document.body) field.current?.focus(); });
  }
  function openList() {
    if (disabled) return;
    setWidth(field.current?.getBoundingClientRect().width ?? 0);
    setOpen(true);
    focusOpenedField(() => search.current);
  }
  function pick(option: Option) {
    if (!multiple) {
      if (values[0] !== option.id) onChange([option.id]);
      close();
      return;
    }
    if (!values.includes(option.id) && values.length < (max ?? Number.MAX_SAFE_INTEGER)) onChange([...values, option.id]);
    setQuery("");
    setActive(-1);
    search.current?.focus();
  }
  function remove(id: string) {
    onChange(values.filter((value) => value !== id));
    search.current?.focus();
  }
  function keys(event: KeyboardEvent<HTMLInputElement>) {
    if (imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!shown.length) return;
      const step = event.key === "ArrowDown" ? 1 : -1;
      setActive((current) => (current < 0 ? (step > 0 ? 0 : shown.length - 1) : (current + step + shown.length) % shown.length));
    } else if (fieldEnter(event)) {
      // Enter picks here; it never sends the form.
      event.preventDefault();
      const option = shown[active];
      if (option) pick(option);
    } else if (event.key === "Backspace" && !query && values.length) {
      event.preventDefault();
      remove(values[values.length - 1]!);
    } else if (event.key === "Tab") {
      event.preventDefault();
      close();
    }
  }

  const place = usePopoverPlacement(list, open ? (size, viewport) => {
    const rect = field.current?.getBoundingClientRect();
    return placeAtPoint({ x: rect ? inlineStartX(rect, 0, field.current) : 0, y: rect?.top ?? 0 }, size, viewport);
  } : null, [open], undefined, { kind: "picker", restoreFocus: false, onEscape: () => close(), onOutsidePress: () => close(false), inside: () => [field.current] });

  return (
    <>
      <button ref={field} type="button" className={styles.field} aria-label={label} aria-haspopup="dialog" aria-expanded={open}
        disabled={disabled} data-form-dropdown-field onClick={() => (open ? close() : openList())}>
        {chosen.length ? (
          <span className={styles.fieldChips}>
            {chosen.map((option) => <span key={option.id} className={styles.chip} data-form-dropdown-chip={option.id}>{option.name}</span>)}
          </span>
        ) : <span className={styles.placeholder}>{placeholder}</span>}
        <ChevronUpDown size="small" className={styles.fieldIcon} aria-hidden="true" />
      </button>
      {open ? createPortal(
        <div ref={list} role="dialog" aria-label={label} className={styles.list} style={{ ...popoverPlacementStyle(place), width: width || undefined }}
          data-form-dropdown-list>
          <div className={styles.search}>
            {chosen.map((option) => (
              <span key={option.id} className={styles.searchChip} data-form-dropdown-chip={option.id}>
                {option.name}
                <button type="button" className={styles.chipRemove} aria-label={t("dropdownRemove", { name: option.name })}
                  onClick={() => remove(option.id)}><X size="small" aria-hidden="true" /></button>
              </span>
            ))}
            <input ref={search} className={styles.searchInput} value={query} role="combobox" aria-expanded="true" aria-controls={listId}
              aria-label={t("dropdownSearch")} aria-activedescendant={shown[active] ? `${listId}-${shown[active].id}` : undefined}
              placeholder={chosen.length ? "" : t("dropdownSearch")}
              onChange={(event) => { setQuery(event.target.value); setActive(event.target.value.trim() ? 0 : -1); }} onKeyDown={keys} />
          </div>
          <div className={styles.options}>
            <div className={styles.heading} aria-hidden="true">{shown.length ? t("dropdownHeading") : t("dropdownNoMatch")}</div>
            <div id={listId} role="listbox" aria-label={label} aria-multiselectable={multiple || undefined}>
              {shown.map((option, index) => (
                <div key={option.id} id={`${listId}-${option.id}`} role="option" aria-selected={values.includes(option.id)}
                  className={styles.option} data-active={index === active ? "true" : undefined} data-form-dropdown-option={option.id}
                  onPointerMove={() => setActive(index)} onMouseDown={(event) => event.preventDefault()} onClick={() => pick(option)}>
                  {option.name}
                </div>
              ))}
            </div>
          </div>
        </div>, document.body) : null}
    </>
  );
}

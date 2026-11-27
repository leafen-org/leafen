import { useEffect, useId, useRef, useState, type KeyboardEvent, type UIEvent } from "react";
import { createPortal } from "react-dom";
import { Trans, useTranslation } from "react-i18next";
import { CheckIcon, ChevronDown, ChevronUpDown, FilterIcon, MinusIcon, X } from "@/icons/leafen";
import { DataSourceMark } from "../database/DataSourceMark";
import { Plus } from "../icons";
import { PageIconGlyph } from "../page/PageIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { placeAtPoint, popoverPlacementStyle, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { FormReferenceOption } from "@/lib/types";
import dropdown from "./FormDropdown.module.css";
import styles from "./FormReferencePicker.module.css";
import { inlineStartX } from "@/lib/readingDirection";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { focusOpenedField } from "@/components/editor/openedFieldCaret";

// A typed search asks the server once typing rests.
const SEARCH_DELAY_MS = 200;
// The next window is asked for this near the list's end.
const LOAD_MORE_REACH = 80;

type PageFilter = "all" | "verified" | "archived";
/** The database a relation's answers come from, by its source's name and icon. */
export type FormReferenceTarget = { title: string; icon?: string; iconType?: string };
// Which pages a relation's list reads, as Notion's (seen 2026-11-05).
const PAGE_FILTERS = [[undefined, "pageFilterActive"], ["all", "pageFilterAll"], ["verified", "pageFilterVerified"], ["archived", "pageFilterArchived"]] as const;

/**
 * A form's relation or person answer as Notion's respondents see it (seen
 * 2026-11-04). Closed, a relation's field says "N개 선택됨" with the chosen
 * pages under it, each with ×, and a person's holds the chosen people. Opened,
 * a list lies over the field, or above it where there is no room: a relation's
 * has a "검색" line, then "페이지 선택" — or "N개 선택함" with the chosen
 * pages and "추가 선택" with the rest — each page with its icon and, under the
 * pointer, + or −; a person's has a search line holding the chosen people,
 * each with ×, then the others. A search asks the server for what has it; the
 * list asks for more near its end. One of at most one replaces the answer and
 * closes; Escape closes back to the field.
 */
export function FormReferencePicker({
  id, kind, label, placeholder, value, disabled, options, loading, error, hasMore, taskDatabase, target, respondentId, max, onRequest, onChange,
}: {
  id: string;
  kind: "relation" | "person";
  label: string;
  placeholder: string;
  value: unknown;
  disabled: boolean;
  options: FormReferenceOption[] | undefined;
  loading: boolean;
  error: string | undefined;
  hasMore: boolean;
  /** The pages are a task database's: an untitled one is "새 작업". */
  taskDatabase: boolean;
  /** The database a relation's list reads: "〈이름〉 내 검색", its source's mark before the name. */
  target?: FormReferenceTarget;
  /** Who answers, named "〈이름〉(나)" in a person's list. */
  respondentId?: string;
  max: number;
  onRequest: (append: boolean, query: string, pageFilter?: PageFilter) => void;
  onChange: (value: string[]) => void;
}) {
  const { t } = useTranslation(["formView", "common"]);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(-1);
  const [width, setWidth] = useState(0);
  const [pageFilter, setPageFilter] = useState<PageFilter | undefined>();
  const [filtering, setFiltering] = useState(false);
  const field = useRef<HTMLButtonElement>(null), list = useRef<HTMLDivElement>(null), search = useRef<HTMLInputElement>(null);
  const filterButton = useRef<HTMLButtonElement>(null), filterMenu = useRef<HTMLDivElement>(null);
  const asked = useRef<string | null>(null);
  const request = useRef(onRequest);
  request.current = onRequest;
  const filterNow = useRef(pageFilter);
  filterNow.current = pageFilter;
  const listId = useId();
  const selected = Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
  // A chosen one keeps its name once a search no longer lists it.
  const known = useRef(new Map<string, FormReferenceOption>());
  for (const option of options ?? []) if (selected.includes(option.id)) known.current.set(option.id, option);
  const chosen = selected.map((chosenId) => known.current.get(chosenId) ?? { id: chosenId, name: "" });
  const name = (option: FormReferenceOption) => kind === "person" && option.id === respondentId
    ? t("formView:meName", { name: option.name || option.id })
    : option.name || (kind === "person" ? option.id : taskDatabase ? t("formView:newTask") : untitledPageDisplayTitle("page"));
  const typed = query.trim().toLocaleLowerCase();
  const others = (options ?? []).filter((option) => !selected.includes(option.id));
  // A relation lists its chosen pages first; a person's chosen ones are in the search line.
  const rows = kind === "relation"
    ? [...chosen.filter((option) => !typed || name(option).toLocaleLowerCase().includes(typed)), ...others]
    : others;
  const chosenShown = kind === "relation" ? rows.length - others.length : 0;

  // The list asks again once what it had is gone (the form read again).
  const missing = options === undefined;
  useEffect(() => {
    if (!open) return;
    const wanted = query.trim();
    if (asked.current === wanted && !missing) return;
    const timer = window.setTimeout(() => { asked.current = wanted; request.current(false, wanted, filterNow.current); },
      asked.current === null || asked.current === wanted ? 0 : SEARCH_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [missing, open, query]);

  function close(back = true) {
    setOpen(false);
    setQuery("");
    setActive(-1);
    if (!back) return;
    field.current?.focus();
    window.requestAnimationFrame(() => { if (document.activeElement === document.body) field.current?.focus(); });
  }
  function openList() {
    if (disabled) return;
    setWidth(field.current?.getBoundingClientRect().width ?? 0);
    // A list last searched asks again for everything.
    if (asked.current !== "" || options === undefined) asked.current = null;
    setOpen(true);
    focusOpenedField(() => search.current);
  }
  function toggle(option: FormReferenceOption) {
    known.current.set(option.id, option);
    if (selected.includes(option.id)) {
      onChange(selected.filter((chosenId) => chosenId !== option.id));
    } else if (max === 1) {
      onChange([option.id]);
      close();
      return;
    } else if (selected.length < max) {
      onChange([...selected, option.id]);
    }
    if (kind === "person") setQuery("");
    search.current?.focus();
  }
  function remove(optionId: string) {
    onChange(selected.filter((chosenId) => chosenId !== optionId));
  }
  function keys(event: KeyboardEvent<HTMLInputElement>) {
    if (imeComposing(event)) return;
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!rows.length) return;
      const step = event.key === "ArrowDown" ? 1 : -1;
      setActive((current) => (current < 0 ? (step > 0 ? 0 : rows.length - 1) : (current + step + rows.length) % rows.length));
    } else if (fieldEnter(event)) {
      // Enter picks here; it never sends the form.
      event.preventDefault();
      const option = rows[active];
      if (option) toggle(option);
    } else if (event.key === "Backspace" && kind === "person" && !query && selected.length) {
      event.preventDefault();
      remove(selected[selected.length - 1]!);
    } else if (event.key === "Tab") {
      event.preventDefault();
      close();
    }
  }
  function scrolled(event: UIEvent<HTMLDivElement>) {
    const box = event.currentTarget;
    if (hasMore && !loading && box.scrollHeight - box.scrollTop - box.clientHeight < LOAD_MORE_REACH) onRequest(true, query.trim(), pageFilter);
  }

  const place = usePopoverPlacement(list, open ? (size, viewport) => {
    const rect = field.current?.getBoundingClientRect();
    return placeAtPoint({ x: rect ? inlineStartX(rect, 0, field.current) : 0, y: rect?.top ?? 0 }, size, viewport);
  } : null, [open], undefined, { kind: "picker", restoreFocus: false, onEscape: () => close(), onOutsidePress: () => close(false), inside: () => [field.current, filterMenu.current] });

  const row = (option: FormReferenceOption, index: number) => {
    const picked = selected.includes(option.id);
    return (
      <div key={option.id} id={`${listId}-${option.id}`} role="option" aria-selected={picked}
        className={kind === "relation" ? styles.page : styles.person} data-active={index === active ? "true" : undefined}
        data-form-reference-option={option.id} onPointerMove={() => setActive(index)}
        onMouseDown={(event) => event.preventDefault()} onClick={() => toggle(option)}>
        {kind === "relation"
          ? <span className={styles.pageIcon} aria-hidden="true"><PageIconGlyph page={{ icon: option.icon ?? "", iconType: (option.iconType || "none") as "none", hasContent: option.hasContent === true }} size={18} /></span>
          : <PersonAvatar userId={option.id} avatar={option.avatar} name={name(option)} size={20} />}
        <span className={styles.name}>{name(option)}</span>
        {kind === "relation" ? (
          <span className={styles.rowAction} aria-hidden="true">{picked ? <MinusIcon size="small" /> : <Plus size="small" />}</span>
        ) : null}
      </div>
    );
  };
  const heading = (key: string, text: string) => <div key={key} className={dropdown.heading} aria-hidden="true">{text}</div>;

  return (
    <>
      <button ref={field} id={id} type="button" className={dropdown.field} aria-label={label} aria-haspopup="dialog" aria-expanded={open}
        disabled={disabled} data-form-reference-field={kind} onClick={() => (open ? close() : openList())}>
        {kind === "person" && chosen.length ? (
          <span className={dropdown.fieldChips}>
            {chosen.map((option) => (
              <span key={option.id} className={styles.personChip} data-form-reference-chip={option.id}>
                <PersonAvatar userId={option.id} avatar={option.avatar} name={name(option)} size={20} />{name(option)}
              </span>
            ))}
          </span>
        ) : (
          <span className={chosen.length ? styles.count : dropdown.placeholder}>
            {chosen.length ? t("formView:selectedCount", { count: chosen.length }) : placeholder}
          </span>
        )}
        {kind === "person" && chosen.length ? null : <ChevronUpDown size="small" className={dropdown.fieldIcon} aria-hidden="true" />}
      </button>
      {kind === "relation" && chosen.length ? (
        <div className={styles.pickedPages}>
          {chosen.map((option) => (
            <span key={option.id} className={styles.pickedPage} data-form-reference-chip={option.id}>
              <span className={styles.pageIcon} aria-hidden="true"><PageIconGlyph page={{ icon: option.icon ?? "", iconType: (option.iconType || "none") as "none", hasContent: option.hasContent === true }} size={16} /></span>
              {name(option)}
              <button type="button" className={styles.pickedRemove} aria-label={t("formView:dropdownRemove", { name: name(option) })}
                disabled={disabled} onClick={() => remove(option.id)}><X size="micro" aria-hidden="true" /></button>
            </span>
          ))}
        </div>
      ) : null}
      {open ? createPortal(
        <div ref={list} role="dialog" aria-label={label} className={dropdown.list} style={{ ...popoverPlacementStyle(place), width: width || undefined }}
          data-form-reference-list={kind}>
          <div className={kind === "relation" ? styles.pageSearch : dropdown.search}>
            {kind === "person" ? chosen.map((option) => (
              <span key={option.id} className={styles.searchPerson} data-form-reference-chip={option.id}>
                <PersonAvatar userId={option.id} avatar={option.avatar} name={name(option)} size={20} />{name(option)}
                <button type="button" className={dropdown.chipRemove} aria-label={t("formView:dropdownRemove", { name: name(option) })}
                  onClick={() => { remove(option.id); search.current?.focus(); }}><X size="small" aria-hidden="true" /></button>
              </span>
            )) : null}
            <input ref={search} className={dropdown.searchInput} value={query} role="combobox" aria-expanded="true" aria-controls={listId}
              aria-label={label} aria-activedescendant={rows[active] ? `${listId}-${rows[active].id}` : undefined}
              placeholder={kind === "person" && chosen.length ? "" : t(kind === "relation" ? "formView:referenceSearch" : "formView:peopleSearch")}
              onChange={(event) => { setQuery(event.target.value); setActive(event.target.value.trim() ? 0 : -1); }} onKeyDown={keys} />
            {kind === "relation" ? <>
              <span className={styles.filterWrap}>
                <button ref={filterButton} type="button" className={styles.filterButton} aria-label={t("formView:pageFilter")} aria-haspopup="menu" aria-expanded={filtering}
                  data-form-reference-filter onMouseDown={(event) => event.preventDefault()} onClick={() => setFiltering((current) => !current)}>
                  <FilterIcon size="small" aria-hidden="true" /><ChevronDown size="tiny" aria-hidden="true" />
                </button>
                {/* Fixed inside the list, whose edge would cut it off otherwise. */}
                {filtering && filterButton.current && list.current ? createPortal(
                  <div ref={filterMenu} className={styles.filterMenu} role="menu" aria-label={t("formView:pageFilter")}
                    style={{ top: filterButton.current.getBoundingClientRect().bottom + 4, left: Math.max(8, Math.min(filterButton.current.getBoundingClientRect().left, window.innerWidth - 268)) }}>
                    {PAGE_FILTERS.map(([filter, key]) => (
                      <button key={key} type="button" role="menuitemradio" aria-checked={pageFilter === filter} className={styles.filterItem} data-form-page-filter={filter ?? "active"}
                        onMouseDown={(event) => event.preventDefault()} onClick={() => {
                          setFiltering(false);
                          setPageFilter(filter);
                          asked.current = query.trim();
                          onRequest(false, query.trim(), filter);
                        }}>
                        <span className={styles.filterCheck} aria-hidden="true">{pageFilter === filter ? <CheckIcon size="small" /> : null}</span>{t(`formView:${key}`)}
                      </button>
                    ))}
                  </div>, list.current,
                ) : null}
              </span>
              {target?.title ? <span className={styles.searchWhere}><DataSourceMark source={target} />
                <Trans t={t} i18nKey="formView:searchInDatabase" values={{ name: target.title }} components={{ b: <b /> }} /></span> : null}
            </> : null}
          </div>
          <div className={styles.options} onScroll={scrolled}>
            <div id={listId} role="listbox" aria-label={label} aria-multiselectable={max !== 1 || undefined}>
              {kind === "relation" && rows.length
                ? heading("head", chosenShown ? t("formView:pagesChosen", { count: chosenShown }) : t("formView:pagesPick")) : null}
              {rows.map((option, index) => index === chosenShown && chosenShown > 0
                ? [heading("more", t("formView:pagesMore")), row(option, index)]
                : row(option, index))}
            </div>
            {error ? (
              <div className={styles.state} role="alert">
                <span>{error}</span>
                <button type="button" onClick={() => onRequest(false, query.trim())}>{t("common:retry")}</button>
              </div>
            ) : loading ? <p className={styles.state} role="status">{t("formView:loadingChoices")}</p>
              : !rows.length ? <p className={styles.state}>{t("formView:noChoices")}</p> : null}
          </div>
        </div>, document.body) : null}
    </>
  );
}

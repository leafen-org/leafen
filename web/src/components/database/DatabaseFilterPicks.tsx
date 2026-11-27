import { useRef, useState } from "react";
import { CheckIcon, X } from "@/icons/leafen";
import { SearchClearButton } from "@/components/PageActionSearch";
import { filterPickId, filterPickValue, type FilterPick } from "./DatabaseFilterValueInput";
import { databaseViewLabels } from "./databaseViewLabels";
import { relationPagesFound } from "./relationPageOrder";
import styles from "./DatabaseQuickFilterBar.module.css";
import noteStyles from "./DatabaseFilterPicks.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

const pickLabel = (pick: FilterPick) => pick.label;

/** A pick's mark after its name, in the list and its chip alike: a template's own page's "이 템플릿", as a row named alike isn't. */
const pickNote = (pick: FilterPick) => pick.note ? <small className={noteStyles.note}>{pick.note}</small> : null;

/**
 * People or pages to choose as many of as wanted, as Notion's pill editor
 * (seen 2026-11-04): a field holding the chosen ones as chips with "×" and
 * "ⓧ" clearing them, finding the others, the focus left in it after "×"
 * (seen 2026-11-12); then the list — its relative choices first, without
 * icons — the chosen ones marked "✓" at their end.
 */
export function FilterPicks({ label, placeholder, leads, picks, value, pages, onChange, onCleared }: {
  label: string;
  placeholder: string;
  leads: readonly FilterPick[];
  picks: readonly FilterPick[];
  /** Pages, found where words start (relationPagesFound); people anywhere in a name. */
  pages?: boolean;
  value: unknown;
  onChange(value: unknown[] | undefined): void;
  /** After "ⓧ" emptied it: a pill's editor closes, as Notion's (seen 2026-11-12). */
  onCleared?(): void;
}) {
  const labels = databaseViewLabels();
  const [query, setQuery] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);
  const chosen = (Array.isArray(value) ? value : value == null || value === "" ? [] : [value]).map(filterPickId).filter(Boolean);
  const all = [...leads, ...picks];
  const named = chosen.map((id) => all.find((pick) => pick.id === id) ?? { id, label: id });
  const toggle = (id: string) => {
    const next = chosen.includes(id) ? chosen.filter((item) => item !== id) : [...chosen, id];
    onChange(next.length ? next.map(filterPickValue) : undefined);
  };
  const found = query.trim().toLocaleLowerCase();
  const shown = pages ? relationPagesFound(all, query, pickLabel) : all.filter((pick) => !found || pick.label.toLocaleLowerCase().includes(found));
  // The first lit, the arrows moving it and Enter choosing it, as the options' list (Notion's, seen 2026-11-06).
  const [lit, setLit] = useState(0);
  const litIndex = Math.min(lit, shown.length - 1);
  const row = (pick: FilterPick, index: number) => (
    <button key={pick.id} id={`filter-pick-${pick.id}`} type="button" role="option" aria-selected={chosen.includes(pick.id)} className={styles.editorOption}
      data-filter-pick={pick.id} data-page={pick.page && pick.icon ? "" : undefined} data-lit={index === litIndex ? "true" : undefined}
      onPointerMove={() => { if (index !== litIndex) setLit(index); }} onClick={() => toggle(pick.id)}>
      {pick.icon ? <span className={styles.editorPickIcon} aria-hidden="true">{pick.icon}</span> : null}
      <span className={styles.editorPickLabel}>{pick.label}{pickNote(pick)}</span>
      {chosen.includes(pick.id) ? <CheckIcon size="small" className={styles.editorPickCheck} aria-hidden="true" /> : null}
    </button>
  );
  return (
    <div className={styles.editorOptions} data-filter-picks="">
      <div className={styles.editorOptionSearch}>
        {named.map((pick) => (
          <span key={pick.id} className={styles.editorPick} data-with-icon={"chipIcon" in pick && pick.chipIcon ? "" : undefined}
            data-page={"page" in pick && pick.page ? "" : undefined}>
            {"chipIcon" in pick && pick.chipIcon ? <span className={styles.editorPickIcon} aria-hidden="true">{pick.chipIcon}</span> : null}
            <span>{pick.label}{pickNote(pick)}</span>
            <button type="button" aria-label={labels.sheetRemoveChoice(pick.label)} onClick={() => { toggle(pick.id); inputRef.current?.focus(); }}>
              <X size="micro" aria-hidden="true" />
            </button>
          </span>
        ))}
        <input ref={inputRef} value={query} placeholder={named.length ? "" : placeholder} aria-label={label}
          aria-activedescendant={shown[litIndex] ? `filter-pick-${shown[litIndex]!.id}` : undefined}
          onChange={(event) => { setQuery(event.target.value); setLit(0); }}
          onKeyDown={(event) => {
            if (imeComposing(event)) return;
            if (event.key === "ArrowDown" || event.key === "ArrowUp") {
              event.preventDefault();
              if (shown.length) setLit((litIndex + (event.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length);
            } else if (fieldEnter(event) && shown[litIndex]) {
              event.preventDefault(); toggle(shown[litIndex]!.id);
            }
          }} />
        <SearchClearButton value={named.length > 0} small className={styles.editorPickClear} label={labels.sheetClearValue}
          inputRef={onCleared ? undefined : inputRef} onClear={() => { onChange(undefined); onCleared?.(); }} />
      </div>
      <div role="listbox" aria-multiselectable="true" aria-label={label} className={styles.editorOptionList}>
        {shown.map(row)}
      </div>
    </div>
  );
}

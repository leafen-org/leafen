import { useEffect, useState } from "react";
import type { RelativeDateFilterUnit, RelativeDateFilterValue } from "@/lib/types";
import { isRelativeDateFilterValue } from "@/shared/database/queryRelativeDate.mjs";
import { CountChevronDown } from "@/icons/leafen";
import { NotionSelect } from "./NotionSelect";
import { relativeDateUnitLabels } from "./databaseFilterLabels";
import { databaseViewLabels } from "./databaseViewLabels";
import styles from "./databaseSheet";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

const DEFAULT_RELATIVE_DATE_FILTER_VALUE: RelativeDateFilterValue = {
  direction: "past",
  count: 1,
  unit: "week",
};

// Its choices' lists: 180px, right under the button (Notion's, seen 2026-11-06).
const RELATIVE_MENU = { width: 180, maxHeight: 270, gap: 0 };
// Its choices' ⌄, wherever it is set: Notion's thin one 16 tall and as wide as its 9.88 of ink
// (arrowChevronSingleDownSmall, seen 2026-11-22 at 1440 dark, dpr 2, in a quick and an advanced filter alike).
const RELATIVE_CARET = <CountChevronDown size="small" trim aria-hidden="true" />;

/**
 * A relative date's three controls: 지난·다음·이번, a count after 지난 and
 * 다음, and a unit ("월" after 이번 and a count of 1, "개월" after a larger
 * count, relativeDateUnitLabels), as Notion's (seen 2026-10-01, 2026-11-11).
 */
export function RelativeDateFilterValueInput({
  label,
  value,
  onChange,
}: {
  label: string;
  value: unknown;
  onChange: (value: RelativeDateFilterValue) => void;
}) {
  const labels = databaseViewLabels();
  const normalized = isRelativeDateFilterValue(value)
    ? value as RelativeDateFilterValue
    : DEFAULT_RELATIVE_DATE_FILTER_VALUE;
  const directionOptions = (["past", "next", "this"] as const).map((direction) => ({
    value: direction,
    label: labels.relativeDateDirections[direction],
  }));
  const unitLabels = relativeDateUnitLabels(normalized);
  const unitOptions = (["day", "week", "month", "year"] as const).map((unit) => ({
    value: unit,
    label: unitLabels[unit],
  }));

  return (
    <div className={styles.relativeDateValue} data-direction={normalized.direction}>
      <NotionSelect firstLit menuSize={RELATIVE_MENU} menuClassName={styles.relativeChoiceMenu} optionClassName={styles.relativeChoiceOption}
        caret={RELATIVE_CARET} ariaLabel={labels.relativeDateDirectionFor(label)}
        value={normalized.direction}
        options={directionOptions}
        onChange={(direction) => {
          if (direction === "this") {
            onChange({ direction, unit: normalized.unit });
            return;
          }
          onChange({
            direction: direction as "past" | "next",
            count: normalized.direction === "this" ? 1 : normalized.count,
            unit: normalized.unit,
          });
        }}
      />
      {normalized.direction !== "this" ? (
        <RelativeDateCountInput
          ariaLabel={labels.relativeDateCountFor(label)}
          value={normalized.count}
          onChange={(count) => onChange({ ...normalized, count })}
        />
      ) : null}
      <NotionSelect firstLit menuSize={RELATIVE_MENU} menuClassName={styles.relativeChoiceMenu} optionClassName={styles.relativeChoiceOption}
        caret={RELATIVE_CARET} ariaLabel={labels.relativeDateUnitFor(label)}
        value={normalized.unit}
        options={unitOptions}
        onChange={(unit) => onChange({ ...normalized, unit: unit as RelativeDateFilterUnit })}
      />
    </div>
  );
}

function RelativeDateCountInput({
  ariaLabel,
  value,
  onChange,
}: {
  ariaLabel: string;
  value: number;
  onChange: (value: number) => void;
}) {
  const [draft, setDraft] = useState(String(value));
  useEffect(() => setDraft(String(value)), [value]);

  function commit() {
    const next = Number(draft);
    if (Number.isSafeInteger(next) && next > 0) {
      setDraft(String(next));
      if (next !== value) onChange(next);
    } else {
      setDraft(String(value));
    }
  }

  return (
    <input
      className={styles.relativeDateCount}
      type="number"
      min={1}
      step={1}
      inputMode="numeric"
      aria-label={ariaLabel}
      value={draft}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => {
        if (imeComposing(event)) return;
        if (fieldEnter(event)) event.currentTarget.blur();
        else if (event.key === "Escape") {
          setDraft(String(value));
          event.currentTarget.blur();
        }
      }}
    />
  );
}

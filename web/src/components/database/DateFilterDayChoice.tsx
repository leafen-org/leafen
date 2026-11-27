import { isRelativeFilterDay, RELATIVE_FILTER_DAYS } from "@/shared/database/queryRelativeDate.mjs";
import { SidebarChevronDown } from "@/icons/leafen";
import { NotionSelect, type NotionSelectOption } from "./NotionSelect";
import { dateFilterLabels } from "./databaseFilterLabels";
import sheet from "./databaseSheet";

const CUSTOM = "custom";

// Its list: 180px right under its button in a rule (Notion's, seen
// 2026-11-11). In a pill's editor it opens under the whole day field, 240
// wide with its middle on the field's and 6 under it, its 28px rows a pixel
// apart 4 in, and only a tick marks the chosen one: none is lit as it opens
// (Notion's, seen 2026-11-22 at 1440 dark, dpr 1, two states: a 236 field at
// 515..751 over 340..368, the list at 513..753 from 374).
const RULE_MENU = { width: 180, maxHeight: 270, gap: 0 };
const FIELD_MENU_WIDTH = 240;
const FIELD_MENU_UNDER = { of: (button: HTMLElement) => button.closest("[data-day-field]"), gap: 6 };
// In a pill's editor its ⌄ is Notion's filled one at 12 (arrowChevronSingleDownFillSmall, 7.6 by 4.38 of ink), the
// head's own, named day or not (seen 2026-11-22 at 1440 dark, dpr 2); in a rule its rule's thin one (NotionSelectCaret).
const FIELD_CARET = <SidebarChevronDown size="micro" aria-hidden="true" />;

/** The words a day named from today reads as ("1주 전"), or "" for any other value. */
export function relativeFilterDayText(value: unknown): string {
  return isRelativeFilterDay(value) ? dateFilterLabels().relativeDays(value) : "";
}

/**
 * A day condition's choice between a day of its own ("사용자 지정 날짜") and a
 * day named from today — 오늘, 내일, 어제, 1주 전, 1주 후, 한 달 전, 한 달 후 —
 * as Notion's (seen 2026-11-11). Choosing one keeps that word, which the
 * filter reads as that day on the reader's clock every day; choosing
 * "사용자 지정 날짜" from one empties the value, as Notion's. In a rule it is
 * a button of its own before the day, its chosen word lit and unticked; in
 * a pill's editor its list opens under its field (`data-day-field`), ticking
 * the chosen one, and `className` places it.
 */
export function DateFilterDayChoice({ label, value, onChange, inField = false, className, buttonClassName }: {
  label: string;
  value: unknown;
  onChange: (value: string | undefined) => void;
  /** In a pill's editor, at its day field's end. */
  inField?: boolean;
  className?: string;
  buttonClassName?: string;
}) {
  const labels = dateFilterLabels();
  const chosen = isRelativeFilterDay(value) ? value : CUSTOM;
  const options: NotionSelectOption[] = [
    ...RELATIVE_FILTER_DAYS.map((word) => ({ value: word, label: labels.relativeDays(word) })),
    { value: CUSTOM, label: labels.customDay },
  ];
  return (
    <NotionSelect className={className} buttonClassName={buttonClassName} ariaLabel={labels.relativeDayFor(label)}
      value={chosen} options={options} caret={inField ? FIELD_CARET : undefined}
      {...(inField ? { plain: "ticked" as const, menuUnder: FIELD_MENU_UNDER, menuMinWidth: FIELD_MENU_WIDTH }
        : { showCheck: false, menuSize: RULE_MENU, menuMinWidth: 180, menuClassName: sheet.relativeChoiceMenu, optionClassName: sheet.relativeChoiceOption })}
      onChange={(next) => {
        if (next === chosen) return;
        onChange(next === CUSTOM ? undefined : next);
      }} />
  );
}

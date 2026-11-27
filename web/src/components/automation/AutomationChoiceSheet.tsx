import { Fragment, isValidElement, useState, type ReactNode } from "react";
import { CheckIcon, XCircleIcon } from "@/icons/leafen";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { PhoneSheetMenu, PhoneSheetRow, phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import type { AutomationChoice } from "./AutomationChoicePopover";
import styles from "./AutomationChoicePopover.module.css";
import { PhoneSheetSearch } from "@/components/shell/PhoneSheetSearch";

/**
 * A property's mark in a list of properties drawn as the sheet draws its marks — 22px in the icons' colour, as
 * Notion's phone "속성 편집 ⌄" (seen 2026-11-16 on an A90 at 411) — rather than the desktop list's 19.2px grey
 * (AutomationPropertyMark); any other mark is left as it is.
 */
function sheetPropertyMark(icon: ReactNode): ReactNode {
  if (!isValidElement<{ property?: Parameters<typeof PropertyIcon>[0]["property"] }>(icon) || !icon.props.property) return icon;
  return <PropertyIcon property={icon.props.property} size="small" />;
}

/**
 * An automation's choice list on a phone, as Notion's (seen 2026-11-15 on an
 * A90 at 411: "+ 새 조건", "속성 편집 ⌄" and the data sources): rising from
 * the screen's foot with its 45px search first, the keys not in it — no
 * keyboard comes up until the search is pressed — and a row per choice, a
 * second grey line under one that has it; picking one closes it, unless it
 * keeps open for more (the people's).
 */
export function AutomationChoiceSheet({ choices, label, title, searchPlaceholder, properties = false, keepOpen = false, picked, clearLabel, onClear,
  emptyText, onPick, onClose }: {
  choices: AutomationChoice[];
  label: string;
  title?: string;
  searchPlaceholder?: string;
  properties?: boolean;
  keepOpen?: boolean;
  picked?: ReactNode;
  clearLabel?: string;
  onClear?(): void;
  emptyText?: string;
  onPick(key: string): void;
  onClose(): void;
}) {
  const [query, setQuery] = useState("");
  const shown = choices.filter((choice) => choice.text.toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()));
  const search = !searchPlaceholder ? null : picked !== undefined ? <input className={styles.searchInBox} value={query}
    autoFocus={fieldFocusOnOpen()} aria-label={searchPlaceholder} placeholder={picked ? undefined : searchPlaceholder}
    onChange={(event) => setQuery(event.currentTarget.value)} /> : <PhoneSheetSearch value={query} autoFocus={fieldFocusOnOpen()}
    aria-label={searchPlaceholder} placeholder={searchPlaceholder} onValueChange={setQuery} />;
  return (
    <PhoneSheetMenu label={label} marks={properties ? "properties" : undefined} onClose={onClose}>
      {title ? <h3 className={sheetStyles.groupLabel}>{title}</h3> : null}
      {search && picked !== undefined ? (
        <div className={`${sheetStyles.menuSearch} ${styles.searchBox}`}>
          {picked}{search}
          {onClear ? <button type="button" className={styles.clear} aria-label={clearLabel} onClick={onClear}>
            <XCircleIcon size="small" aria-hidden="true" />
          </button> : null}
        </div>
      ) : search}
      {emptyText && !shown.length && query.trim() ? <p className={sheetStyles.groupLabel} role="status">{emptyText}</p> : null}
      {shown.map((choice, index) => (
        <Fragment key={choice.key}>
          {choice.group && choice.group !== shown[index - 1]?.group ? <h3 className={sheetStyles.groupLabel}>{choice.group}</h3> : null}
          <PhoneSheetRow icon={properties ? sheetPropertyMark(choice.icon) : choice.icon} label={choice.text} description={choice.detail} more={Boolean(choice.more)} danger={choice.danger}
            value={choice.chosen ? <CheckIcon size="small" /> : undefined} data={{ "data-automation-choice": choice.key }}
            onPress={() => { if (keepOpen) setQuery(""); else onClose(); onPick(choice.key); }} />
        </Fragment>
      ))}
    </PhoneSheetMenu>
  );
}

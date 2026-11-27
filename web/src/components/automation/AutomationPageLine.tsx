import { useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, Trash } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import type { DbProperty } from "@/lib/types";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { AutomationChoicePopover, AutomationPropertyMark } from "./AutomationChoicePopover";
import styles from "./AutomationAddPage.module.css";

const REMOVE = "\u0000remove";

/**
 * A line of a card that adds or edits pages, as Notion's (seen 2026-11-10 in
 * "페이지 추가 위치" and "페이지 편집 위치"): its property's "[mark] 이름 ⌄"
 * over a 164px column, then its value. The "⌄" opens a 280px window centred
 * under it — "속성을 검색하세요", the properties the card doesn't set yet with
 * their marks, and under a line "제거" — where another property takes the
 * line's place. A value its property can't take turns the name red with a
 * warning and says why under the value; a deleted property's line says so.
 */
export function AutomationPageLine({ property, name, left, labels, problem, onPick, onRemove, children, data }: {
  /** Its property, null once deleted. */
  property: DbProperty | null;
  name: string;
  /** The properties the card doesn't set yet, in the order "속성 편집 ⌄" lists them. */
  left: readonly DbProperty[];
  /** Their names where two read the same (propertyOptionLabels), by id. */
  labels?: Readonly<Record<string, string>>;
  /** Why its value can't be set, under it. */
  problem?: string | null;
  onPick(property: DbProperty): void;
  onRemove(): void;
  children?: ReactNode;
  data?: Record<string, string | undefined>;
}) {
  const { t } = useTranslation("databaseView");
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const label = t("addPageLineMenu", { name });
  return (
    <div className={styles.line} {...data} data-gone={property ? undefined : ""}>
      <span className={styles.labelWrap}>
        <button ref={button} type="button" className={styles.label} aria-haspopup="dialog" aria-expanded={open} aria-label={label}
          data-gone={property ? undefined : ""} data-warn={problem ? "" : undefined} onClick={() => setOpen(!open)}>
          {!property ? null : problem ? <WarningFillIcon size="small" aria-hidden="true" /> : <PropertyIcon property={property} size="small" />}
          <span className={styles.labelName}>{name}</span>
          <ChevronDown size="small" aria-hidden="true" />
        </button>
        {open ? (
          <AutomationChoicePopover label={label} searchPlaceholder={t("searchProperties")} width={280} align="center" flush properties
            trigger={button.current}
            choices={[...left.map((candidate) => ({ key: candidate.id, text: labels?.[candidate.id] ?? candidate.name,
              icon: <AutomationPropertyMark property={candidate} />, wideIcon: true })),
            { key: REMOVE, text: t("addPageRemoveLine"), icon: <Trash size="regular" />, rule: true }]}
            onPick={(key) => {
              if (key === REMOVE) { onRemove(); return; }
              const picked = left.find((candidate) => candidate.id === key);
              if (picked) onPick(picked);
            }}
            onClose={() => setOpen(false)} />
        ) : null}
      </span>
      {children !== undefined || problem ? (
        <span className={styles.value}>
          {children}
          {problem ? <span className={styles.problem} data-automation-warning="formula-type">{problem}</span> : null}
        </span>
      ) : null}
    </div>
  );
}

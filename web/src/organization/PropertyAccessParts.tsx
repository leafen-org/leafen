import { useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { PropertyAccessDialogProperty } from "@/lib/organizationModule";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { CheckIcon, PersonCircleIcon, ServerUsersIcon, Trash } from "@/icons/leafen";
import { directoryPeople } from "@/lib/peopleDirectory";
import { placeAround, popoverPlacementStyle, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import type { PropertyAccessLevel } from "@/lib/propertyAccess";
import { fieldFocusOnOpen } from "@/lib/screenKind";
import type { PropertyAccessException, PropertyAccessGroup, PropertyAccessIntegration, PropertyAccessPrincipalType } from "./propertyAccessApi";
import { LevelPicture } from "./PropertyAccessLevelPicture";
import styles from "./PropertyAccessDialog.module.css";

/** The levels a rule's part offers, as Notion's menus (seen 2026-10-12). */
export function propertyAccessLevels(kind: "default" | PropertyAccessPrincipalType): PropertyAccessLevel[] {
  switch (kind) {
    case "default":
      return ["inherit", "edit", "edit_values", "view", "view_property", "none"];
    case "integration":
      return ["edit", "view", "none"];
    case "person_property":
      return ["edit_values", "view", "none"];
    default:
      return ["edit", "edit_values", "view", "view_property", "none"];
  }
}

/** A level's words: a person property's "view" sees its values only, as Notion's names it. */
export function levelText(t: (key: string) => string, level: PropertyAccessLevel, kind: "default" | PropertyAccessPrincipalType = "default") {
  const key = kind === "person_property" && (level === "view" || level === "none") ? `${level}PersonProperty` : level;
  return { label: t(`level.${key}`), description: t(`level.${key}Description`) };
}

/**
 * A popover of the rule's pickers; `flush` leaves the room to what it holds, as the database's property picker keeps
 * its own; `restoreFocus` gives the focus back to what opened it as it closes (the preview's person button).
 */
export function AccessPopover({ anchor, onClose, children, width, flush = false, className = "", restoreFocus }: {
  anchor: PopoverRect; onClose: () => void; children: ReactNode; width: number; flush?: boolean; className?: string; restoreFocus?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(ref, (size, viewport) => placeAround(anchor, size, viewport, { gap: 4 }), [anchor],
    undefined, { kind: "picker", onEscape: onClose, onOutsidePress: onClose, restoreFocus });
  return createPortal(
    <div ref={ref} className={`${styles.popover} ${flush ? styles.flushPopover : ""} ${className}`} style={{ ...popoverPlacementStyle(place), width }}
      role="dialog">{children}</div>,
    document.body,
  );
}

/** The default level card's row, 8px inside the card, which its menu opens under at its width. */
export function levelCardRow(card: Element): PopoverRect {
  const rect = () => card.getBoundingClientRect();
  return { get top() { return rect().top + 8; }, get bottom() { return rect().bottom - 8; },
    get left() { return rect().left + 8; }, get right() { return rect().right - 8; }, contextElement: card };
}

/**
 * The menu a level's box opens, as Notion's (read 2026-11-18 in Chrome at
 * 1440): the default level's 4px under its card's row at that row's width,
 * an exception's 300 wide; 8px around its levels, 2 apart, each 10px over and
 * under its 37×25 picture, 10 before its name 14px on 20 and what it lets one
 * do 12px on 16 in grey; "예외 제거" for an exception (a phone's: PhoneLevelSheet).
 */
export function LevelMenu({ anchor, kind, value, onChoose, onRemove, onClose }: {
  anchor: PopoverRect;
  kind: "default" | PropertyAccessPrincipalType;
  value: PropertyAccessLevel;
  onChoose: (level: PropertyAccessLevel) => void;
  onRemove?: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("propertyAccess");
  return (
    <AccessPopover anchor={anchor} onClose={onClose} width={kind === "default" ? Math.round(anchor.right - anchor.left) : 300}
      className={styles.levelPopover}>
      <div role="menu" className={styles.menu} data-access-level-menu>
        {propertyAccessLevels(kind).map((level) => {
          const text = levelText(t, level, kind);
          return (
            <button key={level} type="button" role="menuitemradio" aria-checked={level === value} className={styles.levelOption}
              data-access-level={level} onClick={() => { onChoose(level); onClose(); }}>
              <LevelPicture level={level} valuesOnly={kind === "person_property"} />
              <span className={styles.levelText}><span>{text.label}</span><span className={styles.levelDescription}>{text.description}</span></span>
              {level === value ? <CheckIcon size="small" aria-hidden="true" /> : null}
            </button>
          );
        })}
        {onRemove ? <>
          <div className={styles.menuDivider} role="separator" />
          <button type="button" role="menuitem" className={styles.removeOption} data-access-remove onClick={() => { onRemove(); onClose(); }}>
            <Trash size="small" aria-hidden="true" />{t("removeException")}
          </button>
        </> : null}
      </div>
    </AccessPopover>
  );
}

export type PrincipalChoice = { type: PropertyAccessPrincipalType; id: string; label: string; section: "people" | "groups" | "integrations" | "database" };

export type PrincipalPickerProps = {
  taken: readonly PropertyAccessException[];
  groups: readonly PropertyAccessGroup[];
  integrations: readonly PropertyAccessIntegration[];
  personProperties: readonly PropertyAccessDialogProperty[];
  onPick: (choice: Pick<PrincipalChoice, "type" | "id">) => void;
  onClose: () => void;
};

/** The picker's search and what it finds: people, groups, apps and the database's person properties not yet named. */
export function usePrincipalChoices({ taken, groups, integrations, personProperties }: Omit<PrincipalPickerProps, "onPick" | "onClose">) {
  const { t } = useTranslation("propertyAccess");
  const [query, setQuery] = useState("");
  const choices = useMemo<PrincipalChoice[]>(() => {
    const integrationIds = new Set(integrations.map((integration) => integration.userId));
    return [
      ...directoryPeople().filter((person) => !integrationIds.has(person.userId))
        .map((person) => ({ type: "user" as const, id: person.userId, label: person.displayName ?? "", section: "people" as const })),
      ...groups.map((group) => ({ type: "group" as const, id: group.id, label: group.name, section: "groups" as const })),
      ...integrations.map((integration) => ({ type: "integration" as const, id: integration.userId, label: integration.name, section: "integrations" as const })),
      { type: "person_property" as const, id: "page_creator", label: t("pageCreator"), section: "database" as const },
      ...personProperties.map((property) => ({ type: "person_property" as const, id: property.id, label: property.name, section: "database" as const })),
    ];
  }, [groups, integrations, personProperties, t]);
  const needle = query.trim().toLocaleLowerCase();
  const shown = choices.filter((choice) => !taken.some((exception) => exception.principalType === choice.type && exception.principalId === choice.id)
    && (!needle || choice.label.toLocaleLowerCase().includes(needle)));
  return { query, setQuery, shown };
}

/** The picker's sections, in order. */
export const PRINCIPAL_SECTIONS = ["people", "groups", "integrations", "database"] as const;

/** "+ 사용자, 그룹 또는 사용자 속성 추가" (a phone's: PhonePrincipalSheet). */
export function PrincipalPicker({ anchor, onPick, onClose, ...rest }: PrincipalPickerProps & { anchor: PopoverRect }) {
  const { t } = useTranslation("propertyAccess");
  const { query, setQuery, shown } = usePrincipalChoices(rest);
  return (
    <AccessPopover anchor={anchor} onClose={onClose} width={220}>
      <input className={styles.pickerSearch} autoFocus={fieldFocusOnOpen()} value={query} placeholder={t("searchPrincipals")} aria-label={t("searchPrincipals")}
        onChange={(event) => setQuery(event.target.value)} />
      <div role="listbox" className={styles.pickerList}>
        {PRINCIPAL_SECTIONS.map((section) => {
          const rows = shown.filter((choice) => choice.section === section);
          if (!rows.length) return null;
          return (
            <div key={section} role="group" aria-label={t(`section.${section}`)}>
              <div className={styles.pickerSection}>{t(`section.${section}`)}</div>
              {rows.map((choice) => (
                <button key={`${choice.type}:${choice.id}`} type="button" role="option" aria-selected={false} className={styles.pickerOption}
                  data-access-principal={`${choice.type}:${choice.id}`} onClick={() => { onPick(choice); onClose(); }}>
                  <PrincipalIcon type={choice.type} id={choice.id} label={choice.label} />
                  <span>{choice.label}</span>
                </button>
              ))}
            </div>
          );
        })}
      </div>
    </AccessPopover>
  );
}

export function PrincipalIcon({ type, id, label, propertyType }: { type: PropertyAccessPrincipalType; id: string; label: string; propertyType?: PropertyAccessDialogProperty["type"] }) {
  if (type === "user" || type === "integration") return <PersonAvatar userId={id} name={label} size={20} />;
  if (type === "group") return <span className={styles.principalGlyph}><ServerUsersIcon size="small" aria-hidden="true" /></span>;
  if (id === "page_creator") return <span className={styles.principalGlyph}><PersonCircleIcon size="small" aria-hidden="true" /></span>;
  return <span className={styles.principalGlyph}><PropertyTypeIcon type={propertyType ?? "person"} /></span>;
}

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { RestoreIcon, Trash } from "@/icons/leafen";
import type { AutomationAction, DbProperty } from "@/lib/types";
import type { DatabaseDeletedPropertiesSurface } from "@/lib/databaseDeletedProperties";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { AutomationCardMenu, AutomationFloatingMenus, AutomationPhoneSheets, type AutomationMenuChoice } from "./AutomationActionMenus";
import styles from "./AutomationGoneProperty.module.css";

/** A property an automation still names after it went to the database's Deleted properties. */
export type AutomationGoneProperty = { id: string; name: string; type: string };

/**
 * The deleted properties an open automation names: each one's name and type
 * once Deleted properties are read, and its restore where the viewer may.
 */
export type AutomationGoneProperties = {
  of(propertyId: string): AutomationGoneProperty | undefined;
  restore?(propertyId: string): void;
};

export const AutomationGonePropertiesContext = createContext<AutomationGoneProperties | null>(null);

/** The properties a draft's conditions and "속성 편집" actions name that the database no longer has. */
export function automationGonePropertyIds(propertyIds: readonly string[], actions: readonly AutomationAction[], properties: readonly DbProperty[]) {
  const named = [...propertyIds, ...actions.flatMap((action) => action.type === "edit_property" ? [action.propertyId] : [])];
  return [...new Set(named.filter((id) => id && !properties.some((property) => property.id === id)))].sort();
}

/**
 * Reads Deleted properties, a window at a time, until each property the draft
 * names is found or none is left; restore brings one back with its values,
 * the database's properties read again after it.
 */
export function useAutomationGoneProperties(surface: DatabaseDeletedPropertiesSurface | undefined, missing: readonly string[],
  onError: (reason: unknown) => void): AutomationGoneProperties {
  const [found, setFound] = useState<ReadonlyMap<string, AutomationGoneProperty>>(new Map());
  const key = missing.join("\u0000");
  const errorRef = useRef(onError);
  errorRef.current = onError;
  useEffect(() => {
    if (!surface || !key) return undefined;
    const wanted = new Set(key.split("\u0000"));
    const controller = new AbortController();
    void (async () => {
      const next = new Map<string, AutomationGoneProperty>();
      let cursor: string | undefined;
      do {
        const page = await surface.load(controller.signal, cursor);
        for (const item of page.items) if (wanted.has(item.id)) next.set(item.id, { id: item.id, name: item.name, type: item.type });
        cursor = page.nextCursor;
      } while (cursor && next.size < wanted.size && !controller.signal.aborted);
      if (!controller.signal.aborted) setFound((current) => new Map([...current, ...next]));
    })().catch(() => undefined);
    return () => controller.abort();
  }, [key, surface]);
  const restore = useCallback((propertyId: string) => {
    surface?.restore(propertyId).catch((reason: unknown) => errorRef.current(reason));
  }, [surface]);
  return useMemo(() => ({ of: (propertyId: string) => found.get(propertyId), ...(surface ? { restore } : {}) }), [found, restore, surface]);
}

/** A deleted property's name, or null until Deleted properties say it. */
export function useGonePropertyName(propertyId: string) {
  return useContext(AutomationGonePropertiesContext)?.of(propertyId)?.name ?? null;
}

/** A deleted property's words: "〈이름〉 속성 삭제됨", the name Notion leaves as "{propertyName}" (seen 2026-11-11). */
export function GonePropertyWords({ propertyId }: { propertyId: string }) {
  const { t } = useTranslation("databaseView");
  const name = useGonePropertyName(propertyId);
  return <>{name === null ? t("automationGoneProperty") : t("automationGonePropertyNamed", { name })}</>;
}

/** A deleted property's type mark, 16px; until its type is known, what stood there before (`fallback`). */
export function GonePropertyGlyph({ propertyId, fallback }: { propertyId: string; fallback: ReactNode }) {
  const gone = useContext(AutomationGonePropertiesContext)?.of(propertyId);
  return gone ? <PropertyIcon property={{ type: gone.type }} size="small" /> : <>{fallback}</>;
}

/**
 * A condition's mark for a deleted property, as Notion's (seen 2026-11-11):
 * its type's 16px mark in red on a 20px red ground rounded 4, the box 2px
 * past the mark's place on each side.
 */
export function GonePropertyMark({ propertyId, fallback }: { propertyId: string; fallback: ReactNode }) {
  return <span className={styles.mark} data-automation-gone-mark><GonePropertyGlyph propertyId={propertyId} fallback={fallback} /></span>;
}

/**
 * What pressing a deleted property's card opens, as Notion's (seen
 * 2026-11-11): 266 wide 8px under the card at its start, the first row lit —
 * "↩ 속성 복원" while it can come back, then "🗑 조건 삭제" or "작업 삭제";
 * on a phone the same rows in a sheet. Closed, the focus goes back to the
 * card's button that opened it (`opener`), as Notion's to its card.
 */
export function GonePropertyMenu({ anchor, opener, propertyId, deleteLabel, onDelete, onClose }: {
  anchor: HTMLElement; opener: HTMLElement | null; propertyId: string; deleteLabel: string; onDelete(): void; onClose(): void;
}) {
  const { t } = useTranslation("databaseView");
  const gone = useContext(AutomationGonePropertiesContext);
  const phone = useContext(AutomationPhoneSheets);
  const trigger = useRef<HTMLElement | null>(anchor);
  trigger.current = anchor;
  const pressed = useRef<HTMLElement | null>(opener);
  pressed.current = opener;
  const restore = gone?.restore && gone.of(propertyId) ? gone.restore : undefined;
  const choices: AutomationMenuChoice[] = [
    ...(restore ? [{ key: "restore", label: t("automationRestoreProperty"), icon: <RestoreIcon size="regular" />, run: () => restore(propertyId) }] : []),
    { key: "delete", label: deleteLabel, icon: <Trash size="regular" />, run: onDelete },
  ];
  return <AutomationFloatingMenus.Provider value={!phone}>
    <AutomationCardMenu choices={choices} trigger={trigger} opener={pressed} width={266} gap={8} onClose={onClose} />
  </AutomationFloatingMenus.Provider>;
}

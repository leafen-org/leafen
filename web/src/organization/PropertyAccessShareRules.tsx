import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { DatabasePropertyPicker } from "@/components/database/DatabasePropertyPicker";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import { AddRuleTile, useRuleParts } from "@/components/page/TopBarSharePageRules";
import rulesStyles from "@/components/page/TopBarSharePageRules.module.css";
import { ChevronRight } from "@/icons/leafen";
import type { PropertyAccessShareRulesProps } from "@/lib/organizationModule";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import { PropertyAccessDialog } from "./PropertyAccessDialog";
import { AccessPopover, levelText } from "./PropertyAccessParts";
import { readPagePropertyAccess, type PagePropertyAccess, type PagePropertyAccessProperty } from "./propertyAccessApi";

const key = (item: { dataSourceId: string; propertyId: string }) => `${item.dataSourceId}\u0000${item.propertyId}`;

/**
 * "속성 사용 권한" in a database page's share "고급", as Notion's (seen
 * 2026-10-12): a line for each property with a rule — its icon, name and
 * level, opening its rule — then "새 규칙 추가", which lists the properties
 * without one ("속성 유형 검색...") and opens the rule window for the one
 * chosen. Notion's help mark beside the heading is left out, as page-level
 * rules'.
 */
export function PropertyAccessShareRules({ session, workspaceId, pageId, disabled, onCountChange }: PropertyAccessShareRulesProps) {
  const { t } = useTranslation("propertyAccess");
  const parts = useRuleParts("property");
  const [state, setState] = useState<PagePropertyAccess | null>(null);
  const [failed, setFailed] = useState(false);
  const [picker, setPicker] = useState<PopoverRect | null>(null);
  const [open, setOpen] = useState<PagePropertyAccessProperty | null>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const counted = useRef<number | null>(null);

  const load = useCallback((signal?: AbortSignal) => readPagePropertyAccess(session, pageId, signal).then((next) => {
    if (counted.current !== null && next.rules.length !== counted.current) onCountChange(next.rules.length - counted.current);
    counted.current = next.rules.length;
    setState(next);
    setFailed(false);
  }, () => { if (!signal?.aborted) setFailed(true); }), [onCountChange, pageId, session]);
  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  if (failed && !state) return null;
  const properties = state?.properties ?? [];
  const ruled = new Set(state?.rules.map(key));
  const free = properties.filter((property) => !ruled.has(key(property)));
  const sourceOf = (property: PagePropertyAccessProperty) => properties
    .filter((candidate) => candidate.dataSourceId === property.dataSourceId)
    .map((candidate) => ({ id: candidate.propertyId, name: candidate.name, type: candidate.type, config: { icon: candidate.icon } }));
  return <>
    <div className={parts.label}>{t("title")}</div>
    {(state?.rules ?? []).map((rule) => {
      const property = properties.find((candidate) => key(candidate) === key(rule));
      return property ? <button key={key(rule)} type="button" className={`${parts.row} ${rulesStyles.ruleOpen}`}
        data-share-property-rule={rule.defaultLevel} disabled={disabled} onClick={() => setOpen(property)}>
        {parts.icon(<PropertyIcon property={{ type: property.type, config: { icon: property.icon } }} size="regular" />)}
        <span className={rulesStyles.ruleName}>{property.name}</span>
        <span className={rulesStyles.ruleLevel}>{levelText(t, rule.defaultLevel).label}</span>
        <ChevronRight size="small" aria-hidden="true" />
      </button> : null;
    })}
    <button ref={addRef} type="button" className={parts.add} data-share-property-rule-add aria-expanded={picker !== null}
      disabled={disabled || !state || !free.length} onClick={() => setPicker(popoverRectOf(addRef.current))}>
      <AddRuleTile />
      <span className={rulesStyles.ruleText}><span>{t("addRule")}</span></span>
    </button>
    {picker ? <PropertyPicker anchor={picker} properties={free} phone={parts.phone} onClose={() => { setPicker(null); if (!parts.phone) addRef.current?.focus(); }}
      onChoose={(property) => { setPicker(null); setOpen(property); }} /> : null}
    {open ? <PropertyAccessDialog session={session} workspaceId={workspaceId} dataSourceId={open.dataSourceId}
      properties={sourceOf(open)} personProperties={sourceOf(open).filter((property) => property.type === "person")}
      propertyId={open.propertyId} onSaved={() => void load()} onClose={() => setOpen(null)} /> : null}
  </>;
}

/**
 * The properties without a rule, found by name: Notion's own property picker
 * for these rules is the one its database's filter and sort pickers use (its
 * code, read 2026-11-16), so this is Leafen's (DatabasePropertyPicker) — in
 * the properties' own order, its "결과 없음" row when nothing is found. A
 * phone's is that picker's sheet form in a whole sheet under an empty head,
 * as a sort's, picked at once (Notion's, read 2026-11-18 on a Galaxy A90's
 * Chrome at 411 in a Business database, w1949: no title or "✕", the search
 * and the properties in one white group 16px in, 45px rows).
 */
function PropertyPicker({ anchor, properties, phone, onChoose, onClose }: {
  anchor: PopoverRect;
  properties: PagePropertyAccessProperty[];
  phone: boolean;
  onChoose: (property: PagePropertyAccessProperty) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation("propertyAccess");
  // Each property's own icon rides in its config, as a database's property's does (GraphProperty).
  const choices = properties.map((property) => ({
    id: key(property), name: property.name, type: property.type, config: { icon: property.icon } as Record<string, unknown>,
  }));
  const picker = <DatabasePropertyPicker placeholder={t("searchProperties")} properties={choices} keepOrder sheet={phone} sheetOpensMore={false}
    onPick={(choice) => { const property = properties.find((candidate) => key(candidate) === choice.id); if (property) onChoose(property); }} />;
  if (phone) return <PhoneSheet title={null} label={t("addRule")} onClose={onClose} data={{ "data-share-property-rule-picker": "" }}>{picker}</PhoneSheet>;
  return (
    <AccessPopover anchor={anchor} onClose={onClose} width={240} flush>
      <div data-share-property-rule-picker>{picker}</div>
    </AccessPopover>
  );
}

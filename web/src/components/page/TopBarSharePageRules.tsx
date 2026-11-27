import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { PropertyTypeIcon } from "@/components/database/PropertyTypeIcon";
import { useSidebarLayout } from "@/components/shell/AppShell";
import { CheckIcon, PlusDiamondIcon } from "@/icons/leafen";
import { useOpenedFromKeyboard } from "@/lib/openedFromKeyboard";
import { placeAround, popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { settingsErrorMessage } from "@/lib/settingsErrors";
import {
  publicMutationRoles,
  type PublicMutationPageRuleInput,
  type PublicMutationPageRules,
  type PublicMutationRole,
  type PublicMutationRuleProperty,
} from "@/publicMutationWire.generated";
import { sharePermissionLabel, TopBarSharePermissionPicker } from "./TopBarSharePermissionPicker";
import { SharePhonePersonText } from "./TopBarSharePhone";
import { PhoneRuleCreator } from "./TopBarShareRuleCreatorPhone";
import { soleRuleProperty } from "./topBarShareModel";
import { styles } from "./topBarStyles";
import advanced from "./TopBarShareAdvanced.module.css";
import rulesStyles from "./TopBarSharePageRules.module.css";

/** How the share window reads and changes a database page's rules. */
export type PageRulesAccess = {
  load(signal: AbortSignal): Promise<PublicMutationPageRules>;
  change(input: PublicMutationPageRuleInput): Promise<PublicMutationPageRules>;
  /** The rules changed by this many, for "고급"'s count. */
  countChanged(delta: number): void;
};

type RuleRole = "view" | "comment" | "edit";

const RULE_ROLES: PublicMutationRole[] = [publicMutationRoles.edit, publicMutationRoles.comment, publicMutationRoles.view];

/**
 * "페이지 수준 사용 권한" in a database's "고급", as Notion's (seen
 * 2026-10-12): each rule's line — its property and role, whose menu also
 * deletes it — then "새 규칙 추가" (with an example while there is none),
 * which opens a 308px window to choose a person property — or "생성자",
 * which every database offers; the only one left comes chosen
 * (soleRuleProperty) — and a role and "규칙 생성하기". Notion's
 * help mark beside the heading is left out.
 */
export function TopBarSharePageRules({ rules: rulesAccess, disabled, canAdd = true }: {
  rules: PageRulesAccess; disabled: boolean;
  /** Whether rules are added here: a database's page; a person's page shows People's own alone, as Notion's. */
  canAdd?: boolean;
}) {
  const { t } = useTranslation("topBar");
  const parts = useRuleParts();
  const [rules, setRules] = useState<PublicMutationPageRules | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [adding, setAdding] = useState(false);
  const addRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const controller = new AbortController();
    rulesAccess.load(controller.signal).then(setRules, (failure) => {
      if (!controller.signal.aborted) setError(settingsErrorMessage(failure, t("pageRulesFailed")));
    });
    return () => controller.abort();
  }, [rulesAccess, t]);

  async function change(input: PublicMutationPageRuleInput) {
    setBusy(true);
    setError("");
    try {
      const next = await rulesAccess.change(input);
      rulesAccess.countChanged(next.rules.length - (rules?.rules.length ?? 0));
      setRules(next);
    } catch (failure) {
      setError(settingsErrorMessage(failure, t("pageRulesFailed")));
    } finally {
      setBusy(false);
    }
  }

  const key = (item: { dataSourceId: string; propertyId: string }) => `${item.dataSourceId}\u0000${item.propertyId}`;
  const used = new Set(rules?.rules.map(key));
  const free = (rules?.properties ?? []).filter(property => !used.has(key(property)));
  return <>
    <div className={parts.label}>{t("pageLevelAccess")}</div>
    {(rules?.rules ?? []).map(rule => {
      const property = rules?.properties.find(candidate => key(candidate) === key(rule));
      if (!property) return null;
      const name = property.name || t("ruleCreator");
      const picker = (more: boolean) => <TopBarSharePermissionPicker value={rule.role} disabled={disabled || busy} roles={RULE_ROLES} removeLabel={t("deleteRule")}
        readOnly={rule.fixed === true} more={more} removeMark={more}
        onChange={role => void change({ action: "update", ruleId: rule.id, role: role as RuleRole })}
        onRemove={() => void change({ action: "delete", ruleId: rule.id })} />;
      // A phone's rule reads as a person's line, as Notion's (read 2026-11-18 on a Galaxy A90's Chrome at 411, w1949):
      // the property's mark in its tile, the name over its role, and "⋯" opening "사용 권한 변경" with "권한 규칙 삭제"
      // under a bin; People's own rule, which is fixed, has no "⋯".
      return parts.phone ? <div key={rule.id} className={rulesStyles.phoneRule} data-share-page-rule={rule.role}>
        {parts.icon(<PropertyTypeIcon type={property.type} size="regular" />)}
        <SharePhonePersonText name={name} you={false} role={sharePermissionLabel(rule.role, t)} />
        {rule.fixed ? null : picker(true)}
      </div> : <div key={rule.id} className={parts.row} data-share-page-rule={rule.role}>
        {parts.icon(<PropertyTypeIcon type={property.type} size="regular" />)}
        <span className={rulesStyles.ruleName}>{name}</span>
        {/* People's own rule is fixed: its role as words, no menu, as Notion's (people_database_page_rule.go). */}
        {picker(false)}
      </div>;
    })}
    {canAdd ? <button ref={addRef} type="button" className={parts.add} data-share-page-rule-add aria-expanded={adding}
      disabled={disabled || busy || !rules || !free.length} onClick={() => setAdding(true)}>
      <AddRuleTile />
      <span className={rulesStyles.ruleText}><span>{t("addRule")}</span>{rules?.rules.length || parts.phone ? null : <span>{t("addRuleHint")}</span>}</span>
    </button> : null}
    {error ? <p className={parts.note} role="alert" data-error>{error}</p> : null}
    {adding ? (() => {
      const create = (property: PublicMutationRuleProperty, role: RuleRole) => {
        setAdding(false);
        void change({ action: "create", dataSourceId: property.dataSourceId, propertyId: property.propertyId, role });
      };
      return parts.phone ? <PhoneRuleCreator properties={free} roles={RULE_ROLES} onClose={() => setAdding(false)} onCreate={create} />
        : <RuleCreator anchor={addRef} properties={free} onClose={() => { setAdding(false); addRef.current?.focus(); }} onCreate={create} />;
    })() : null}
  </>;
}

/**
 * The rule sections' parts (page-level and property access), a phone's as
 * Notion's phone "고급" sheet draws them (seen 2026-11-15 at 402pt): the
 * heading 15px, "새 규칙 추가" 52px with a 40px tile at the section's edge and
 * no example under it, and a rule's line with its icon in a grey tile. The
 * property section's lines are 40px, 6px over and under, their words 17px
 * (read 2026-11-18 on a Galaxy A90's Chrome at 411 in a Business database,
 * w1949).
 */
export function useRuleParts(section: "page" | "property" = "page") {
  const { phone } = useSidebarLayout(true);
  const property = section === "property" ? ` ${rulesStyles.phoneProperty}` : "";
  return phone ? {
    phone, label: rulesStyles.phoneLabel, row: `${rulesStyles.phoneRow}${property}`, add: `${rulesStyles.phoneAdd}${property}`, note: rulesStyles.phoneNote,
    icon: (icon: ReactNode) => <span className={rulesStyles.phoneTile} aria-hidden="true">{icon}</span>,
  } : {
    phone, label: advanced.advancedLabel, row: rulesStyles.ruleRow, add: rulesStyles.ruleAdd, note: advanced.advancedNote,
    icon: (icon: ReactNode) => icon,
  };
}

/** "새 규칙 추가"'s blue tile, both rule sections' (page-level and property access). */
export function AddRuleTile() {
  return <span className={rulesStyles.ruleTile} aria-hidden="true"><PlusDiamondIcon size="regular" /></span>;
}

/** The window "새 규칙 추가" opens: a person property, a role, and "규칙 생성하기". */
function RuleCreator({ anchor, properties, onClose, onCreate }: {
  anchor: RefObject<HTMLButtonElement | null>; properties: PublicMutationRuleProperty[];
  onClose(): void; onCreate(property: PublicMutationRuleProperty, role: RuleRole): void;
}) {
  const { t } = useTranslation("topBar");
  const menu = useRef<HTMLDivElement>(null);
  const [chosen, setChosen] = useState<PublicMutationRuleProperty | null>(() => soleRuleProperty(properties));
  // Lit only once pressed: the one it starts on is ticked, not lit (Notion's, 2026-11-18).
  const [pressed, setPressed] = useState(false);
  const [role, setRole] = useState<RuleRole>("edit");
  const placement = usePopoverPlacement(menu, (size, viewport) => placeAround(
    popoverRectOf(anchor.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, size, viewport, { align: "start", gap: 4 },
  ), [], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose, inside: () => [anchor.current] });
  // The window takes the focus, so Escape closes it: pressed open, the window
  // itself, no property lit and a typed letter reaching nothing. The arrows
  // go round the properties and then two stops that show nothing — the role
  // and "규칙 생성하기" — so the first ↓ lights the first property, ↑ from none
  // and ↓ past the last property light nothing, and ↓ from the last stop is
  // back at the first — as Notion's (measured 2026-11-16 in Chrome at 1440 on
  // a database's share › 고급, three tabs at 07:4x, 08:0x and 08:2x, no
  // script in the page before the press, one letter typed after it; the
  // round seen in the last two). Opened with Enter its first property is lit
  // at once, as Notion's was the one time it was seen (07:4x).
  const keysNow = useOpenedFromKeyboard();
  const [byKeys] = useState(keysNow);
  const placed = placement !== null;
  // The stop the arrows are on while the window itself holds the focus: -1
  // for none, past the properties for one of the two that show nothing.
  const quietStop = useRef(-1);
  useEffect(() => {
    if (!placed) return;
    const first = menu.current?.querySelector<HTMLElement>("[role=option]");
    quietStop.current = -1;
    (byKeys && first ? first : menu.current)?.focus({ preventScroll: true });
  }, [placed, byKeys]);
  const moveLight = (event: KeyboardEvent<HTMLDivElement>) => {
    const down = event.key === "ArrowDown";
    if (!down && event.key !== "ArrowUp") return;
    const options = [...menu.current?.querySelectorAll<HTMLElement>("[role=option]") ?? []];
    const lit = options.indexOf(document.activeElement as HTMLElement);
    if (!options.length || (lit < 0 && event.target !== menu.current)) return;
    event.preventDefault();
    const stops = options.length + 2, at = lit >= 0 ? lit : quietStop.current;
    const next = at < 0 ? (down ? 0 : stops - 1) : (at + (down ? 1 : -1) + stops) % stops;
    quietStop.current = next < options.length ? -1 : next;
    (options[next] ?? menu.current)?.focus({ preventScroll: true });
  };
  return createPortal(<div ref={menu} role="dialog" tabIndex={-1} aria-label={t("addRule")} className={`${styles.sharePermissionMenu} ${rulesStyles.ruleCreator}`}
    data-share-page-rule-creator onKeyDown={moveLight} onPointerDown={() => { quietStop.current = -1; }}
    style={{ ...popoverPlacementStyle(placement), position: "fixed", right: "auto", bottom: "auto", visibility: placement ? "visible" : "hidden" }}>
    <div className={rulesStyles.ruleCreatorHeading}>{t("chooseUserProperty")}</div>
    <div role="listbox" aria-label={t("chooseUserProperty")}>
      {properties.map(property => <button key={`${property.dataSourceId}:${property.propertyId}`} type="button" role="option" className={rulesStyles.ruleProperty}
        aria-selected={chosen === property} data-lit={pressed && chosen === property ? "true" : undefined} data-share-page-rule-property={property.type}
        onClick={() => { setChosen(property); setPressed(true); }}>
        <PropertyTypeIcon type={property.type} size="regular" />
        <span>{property.name || t("ruleCreator")}</span>
        {chosen === property ? <CheckIcon size="small" aria-hidden="true" /> : null}
      </button>)}
    </div>
    <p className={rulesStyles.ruleCreatorText}>{t("ruleGrantText")}</p>
    <div className={rulesStyles.ruleCreatorRole}>
      <TopBarSharePermissionPicker value={role} disabled={false} invite layout="full" roles={RULE_ROLES} onChange={next => setRole(next as RuleRole)} />
    </div>
    <button type="button" className={rulesStyles.ruleCreate} disabled={!chosen} data-share-page-rule-create
      onClick={() => { if (chosen) onCreate(chosen, role); }}>
      {t("createRule")}
    </button>
  </div>, document.body);
}

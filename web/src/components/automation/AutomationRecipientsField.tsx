"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, FormulaIcon, PersonCircleIcon, X } from "@/icons/leafen";
import { WarningFillIcon } from "@/icons/leafen/automationIcons";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { PropertyIcon } from "@/components/database/PropertyIcon";
import { useProductPages } from "@/components/product/productPagesContext";
import type { WorkspacePerson } from "@/lib/workspacePeopleChoices";
import type { DbProperty, SendNotificationAutomationAction } from "@/lib/types";
import type { InteractiveAutomationRecipientSource } from "@/interactiveAutomationWire.generated";
import { MAX_RECIPIENTS, type AutomationEditorSurface } from "./automationActionModel";
import { validAutomationRecipientFormula } from "./automationRecipientFormula";
import styles from "./automationActionEditor.module.css";
import recipientStyles from "./AutomationRecipients.module.css";
import { AutomationChoicePopover, AutomationPropertyMark } from "./AutomationChoicePopover";

const sourceKey = (source: InteractiveAutomationRecipientSource) =>
  source.kind === "person_property" ? `property:${source.propertyId}` : source.kind;

/**
 * Fixed people and execution-dependent people share the notification field.
 * Its people, as Notion's (seen 2026-11-08): one ringed box the card's width,
 * "최대 20명 선택... ⌄" until someone is picked, then each by face and name —
 * who runs it or made the page in a blue tag — and the window to pick them
 * under it at its end (over it near the window's foot), staying open while
 * more are picked, those picked in its search with "×". A person property's
 * ("사용자 속성") is one property, as Notion's (seen 2026-11-14): "속성 선택 ⌄",
 * then its mark and name ("👥 Owner ⌄"), picked from the person properties
 * under it — "속성을 검색하세요", each by its mark, the picked one ticked,
 * "결과 없음" for a search that finds none — another picked in its place.
 */
export function AutomationRecipientsField({ action, number, surface, properties, kind = "people", warnEmpty = false, marked = false, onChange }: {
  action: SendNotificationAutomationAction;
  /** People picked by name and who runs it ("특정 사용자"), or a person property of the page ("사용자 속성"), as Notion's chip says. */
  kind?: "people" | "property";
  /** Whether no recipients are shown as a warning, as on a saved notification without them (Notion, seen 2026-10-10). */
  warnEmpty?: boolean;
  /** Naming nobody, its field says so in red under a red mark, as Notion's once asked for (seen 2026-11-01, 2026-11-08). */
  marked?: boolean;
  number: number;
  surface: AutomationEditorSurface;
  properties: DbProperty[];
  onChange: (action: SendNotificationAutomationAction) => void;
}) {
  const { t } = useTranslation(["databaseView"]);
  const load = useProductPages()?.workspacePeople;
  const [people, setPeople] = useState<WorkspacePerson[]>([]);
  const [picking, setPicking] = useState(false);
  const pickerButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    setPeople([]);
    if (!load) return;
    const controller = new AbortController();
    load(controller.signal).then(result => { if (!controller.signal.aborted) setPeople(result); }, () => {});
    return () => controller.abort();
  }, [load]);
  // What the field holds as it is now, for picks made while its window stays open.
  const latest = useRef(action); latest.current = action;
  const { recipientIds } = action;
  const sources = action.recipientSources ?? [];
  const formula = sources.find(source => source.kind === "formula");
  const name = (id: string) => people.find(candidate => candidate.id === id)?.name ?? t("databaseView:recipientUnavailable");
  const sourceName = (source: InteractiveAutomationRecipientSource) => {
    if (source.kind === "person_property") {
      return properties.find(property => property.id === source.propertyId)?.name ?? t("databaseView:recipientUnavailableProperty");
    }
    if (source.kind === "execution_actor" && surface === "event_automation") return t("databaseView:recipientEventActor");
    return t(`databaseView:recipientSource_${source.kind}`);
  };
  const choices = people.filter(({ id }) => !recipientIds.includes(id));
  const personProperties: InteractiveAutomationRecipientSource[] = surface === "page_button" || surface === "schedule_automation" ? []
    : properties.filter(property => property.type === "person").map(property => ({ kind: "person_property" as const, propertyId: property.id }));
  const dynamic: InteractiveAutomationRecipientSource[] = kind === "property" ? personProperties
    : surface === "schedule_automation" ? [] : [{ kind: "execution_actor" }, { kind: "page_creator" }];
  const available = dynamic.filter(candidate => !sources.some(source => sourceKey(source) === sourceKey(candidate)));
  // Its kind goes with what is picked, so taking the last away still names it (automationRecipientKindOf).
  const update = (recipientIds: string[], recipientSources: InteractiveAutomationRecipientSource[]) =>
    onChange({ ...latest.current, recipientIds, recipientSources, recipientKind: kind });
  const empty = !recipientIds.length && !sources.length;
  const warning = marked && empty;
  const pick = (value: string) => {
    const current = latest.current, currentSources = current.recipientSources ?? [];
    if (value === "formula") { setPicking(false); update([], [{ kind: "formula", expression: "" }]); return; }
    if (value.startsWith("person:")) { update([...current.recipientIds, value.slice(7)], currentSources); return; }
    const source = dynamic.find(candidate => sourceKey(candidate) === value);
    if (source && !currentSources.some(candidate => sourceKey(candidate) === value)) update(current.recipientIds, [...currentSources, source]);
  };
  // Each picked one by face and name, who runs it or made the page in a blue tag; in the window each with "×".
  const picked = (removable: boolean): ReactNode[] => [
    ...recipientIds.map(id => <span key={id} className={recipientStyles.recipientToken}>
      <PersonAvatar userId={id} name={name(id)} size={16} />{name(id)}
      {removable ? <button type="button" aria-label={t("databaseView:removeRecipient", { name: name(id) })}
        onClick={() => update(latest.current.recipientIds.filter(candidate => candidate !== id), latest.current.recipientSources ?? [])}>
        <X size="tiny" aria-hidden="true" /></button> : null}
    </span>),
    ...sources.map(source => <span key={sourceKey(source)} className={recipientStyles.recipientSource}>
      <PersonCircleIcon size="tiny" aria-hidden="true" />{sourceName(source)}
      {removable ? <button type="button" aria-label={t("databaseView:removeRecipient", { name: sourceName(source) })}
        onClick={() => update(latest.current.recipientIds,
          (latest.current.recipientSources ?? []).filter(candidate => sourceKey(candidate) !== sourceKey(source)))}>
        <X size="tiny" aria-hidden="true" /></button> : null}
    </span>),
  ];
  const propertyOf = (source: InteractiveAutomationRecipientSource) =>
    source.kind === "person_property" ? properties.find(property => property.id === source.propertyId) : undefined;
  const pickerWindow = !picking ? null : kind === "property" ? (
    <AutomationChoicePopover label={t("databaseView:recipientIdsForAction", { number })} searchPlaceholder={t("databaseView:searchProperties")}
      emptyText={t("databaseView:noSearchResults")} width={280} align="center" flush properties trigger={pickerButton.current}
      choices={personProperties.map((source) => {
        const property = propertyOf(source);
        return { key: sourceKey(source), text: sourceName(source), icon: property ? <AutomationPropertyMark property={property} /> : undefined,
          wideIcon: true, chosen: sources.some(candidate => sourceKey(candidate) === sourceKey(source)) };
      })}
      onPick={(value) => {
        const source = personProperties.find(candidate => sourceKey(candidate) === value);
        if (source) update([], [source]);
      }} onClose={() => setPicking(false)} />
  ) : <AutomationChoicePopover label={t("databaseView:recipientIdsForAction", { number })} width={260}
    title={t("databaseView:recipientsHeading")} align="end"
    searchPlaceholder={t("databaseView:recipientsSearch")} trigger={pickerButton.current}
    keepOpen picked={empty ? null : picked(true)}
    clearLabel={t("databaseView:recipientsClear")} onClear={empty ? undefined : () => update([], [])}
    choices={[
      ...available.map((source) => ({ key: sourceKey(source), text: sourceName(source), icon: <PersonCircleIcon size="regular" /> })),
      ...(recipientIds.length >= MAX_RECIPIENTS ? [] : choices.map((person) => ({
        key: `person:${person.id}`, text: person.name, icon: <PersonAvatar userId={person.id} name={person.name} size={20} />,
      }))),
      { key: "formula", text: t("databaseView:recipientFormulaWrite"), icon: <FormulaIcon size="small" />, more: true, rule: true },
    ]}
    onPick={pick} onClose={() => setPicking(false)} />;
  return (
    <div className={styles.recipients}>
      {formula ? (
        <label className={styles.fieldLabel}>
          {t("databaseView:recipientFormulaForAction", { number })}
          <textarea aria-label={t("databaseView:recipientFormulaForAction", { number })} value={formula.expression}
            aria-invalid={formula.expression.trim() !== "" && !validAutomationRecipientFormula(formula.expression, properties)}
            onChange={event => update([], [{ kind: "formula", expression: event.target.value }])} />
          <p>{t(surface === "schedule_automation" ? "databaseView:recipientFormulaScheduleHelp" : "databaseView:recipientFormulaHelp")}</p>
          {formula.expression.trim() !== "" && !validAutomationRecipientFormula(formula.expression, properties) &&
            <p role="alert">{t("databaseView:recipientFormulaInvalid")}</p>}
        </label>
      ) : (
        <span className={recipientStyles.recipientPicker} data-kind={kind} data-warning={warning ? "" : undefined} data-value={empty ? "" : undefined}>
          <button ref={pickerButton} type="button" className={kind === "property" ? recipientStyles.recipientPropertyButton : recipientStyles.recipientBox}
            aria-label={t("databaseView:recipientIdsForAction", { number })}
            aria-haspopup="dialog" aria-expanded={picking} data-automation-recipients-picker onClick={() => setPicking((open) => !open)}>
            {kind === "people" && !empty ? <span className={recipientStyles.recipientTokens}>{picked(false)}</span>
              : kind === "property" && !empty ? sources.map(source => {
                const property = propertyOf(source);
                return <span key={sourceKey(source)} className={recipientStyles.recipientProperty}>
                  {property ? <span className={recipientStyles.recipientPropertyMark}><PropertyIcon property={property} size="small" /></span> : null}
                  <span className={recipientStyles.recipientPropertyName}>{sourceName(source)}</span></span>;
              }) : <span>
              {warning ? <WarningFillIcon className={recipientStyles.recipientWarningInline} aria-hidden="true" /> : null}
              {t(kind === "property" ? "databaseView:recipientPropertyPlaceholder" : "databaseView:recipientsPlaceholder")}</span>}
            <ChevronDown size={kind === "property" ? "small" : "tiny"} aria-hidden="true" />
          </button>
          {pickerWindow}
        </span>
      )}
      {recipientIds.length >= MAX_RECIPIENTS - 3 ? <p data-automation-recipients-capacity={recipientIds.length >= MAX_RECIPIENTS ? "full" : "near"}>
        {t(recipientIds.length >= MAX_RECIPIENTS ? "databaseView:recipientsPeopleFull" : "databaseView:recipientsPeopleNear",
          { count: recipientIds.length, limit: MAX_RECIPIENTS })}</p> : null}
      {warnEmpty && empty ? <p className={styles.actionWarning} data-automation-warning="recipients">
        {t("databaseView:noRecipients")}</p> : null}
    </div>
  );
}

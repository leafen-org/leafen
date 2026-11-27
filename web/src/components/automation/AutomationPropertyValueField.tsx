"use client";

import { useTranslation } from "react-i18next";
import type { AutomationAction, DbProperty, EditPropertyAutomationAction } from "@/lib/types";
import { usePageNames } from "@/lib/pageNames";
import { useAutomationRelationRows } from "./automationRelationRows";
import styles from "./automationActionEditor.module.css";
import { AppMultiSelect, AppSelect } from "@/components/AppSelect";
import { AutomationDateField } from "./AutomationDateValue";
import { AutomationCheckboxValues } from "./AutomationCheckboxValues";
import type { FormulaScopeName } from "@/components/database/formulaEditorModel";
import {
  AUTOMATION_WORDS_TYPES, AutomationNumberField, AutomationWordsField, automationWordsOf, automationWordsValue, type AutomationFormulaOpen,
} from "./AutomationWordsField";

export function AutomationPropertyValueField({
  action,
  number,
  properties,
  words,
  line,
  onChange,
}: {
  action: EditPropertyAutomationAction;
  number: number;
  properties: DbProperty[];
  /** Words take "@" and Σ, as Notion's (AutomationWordsField): what they mention, and Σ. */
  words?: { names: readonly FormulaScopeName[]; properties: DbProperty[]; onFormula: AutomationFormulaOpen };
  /**
   * In a page's line (a new page's, an edit's), as Notion's (seen
   * 2026-11-10): its words say nothing while empty (a name "제목 없음") and a
   * number is their soft field with Σ; in "속성 편집"'s value window both say
   * "값 입력".
   */
  line?: boolean;
  onChange: (action: AutomationAction) => void;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["databaseView"]);
  const relationRows = useAutomationRelationRows();
  const property = properties.find((candidate) => candidate.id === action.propertyId);
  const literal = action.value.kind === "literal" ? action.value.value : undefined;
  const label = t("databaseView:valueForAction", { number });
  const updateLiteral = (value: unknown) => onChange({ ...action, value: { kind: "literal", value } });

  if (property?.type === "select" || property?.type === "status") {
    return (
      <AppSelect aria-label={label} value={typeof literal === "string" ? literal : ""} onChange={(event) => updateLiteral(event.target.value || null)}>
        <option value="">{t("databaseView:clearValue")}</option>
        {(property.config?.options ?? []).map((option) => <option key={option.id} value={option.id}>{option.name}</option>)}
      </AppSelect>
    );
  }
  if (property?.type === "multi_select") {
    const selected = Array.isArray(literal)
      ? literal.filter((item): item is string => typeof item === "string")
      : [];
    return (
      <AppMultiSelect aria-label={label} placeholder={t("databaseView:automationChooseValues")} values={selected} onChange={updateLiteral}>
        {(property.config?.options ?? []).map((option) => (
          <option key={option.id} value={option.id}>{option.name}</option>
        ))}
      </AppMultiSelect>
    );
  }
  if (property?.type === "relation") {
    const selected = Array.isArray(literal)
      ? literal.filter((item): item is string => typeof item === "string")
      : [];
    const target = property.config?.relationDatabaseId ?? property.databaseId;
    const rows = relationRows?.(target) ?? [];
    return (
      <AppMultiSelect aria-label={label} placeholder={t("databaseView:automationChooseValues")} values={selected} onChange={updateLiteral}>
        {rows.map((row) => <option key={row.id} value={row.id}>{pageNames.title(row)}</option>)}
      </AppMultiSelect>
    );
  }
  // The list says "체크 표시되지 않음", "체크 표시됨" in flat rows; the sentence's chip then
  // "선택 취소됨", "선택됨" (Notion's, seen 2026-11-09).
  if (property?.type === "checkbox") return <AutomationCheckboxValues action={action} label={label} onChange={onChange} />;
  if (property?.type === "number" && line && words) {
    return <AutomationNumberField value={literal} label={label} onFormula={words.onFormula} onChange={updateLiteral} />;
  }
  if (property?.type === "number") {
    return <input type="number" aria-label={label} placeholder={t("databaseView:value")} value={typeof literal === "number" ? literal : ""} onChange={(event) => updateLiteral(event.target.value === "" ? null : Number(event.target.value))} />;
  }
  // A date's value is Notion's list, from a field where a line holds it (AutomationDateValue).
  if (property?.type === "date") {
    return <AutomationDateField action={action} label={label} onFormula={words?.onFormula} onChange={onChange} />;
  }
  // People are picked by name in their own window (AutomationPersonValue), from a card and from a line alike.
  if (property?.type === "verification") {
    const state = literal && typeof literal === "object" && "state" in literal
      ? String((literal as { state?: unknown }).state)
      : "unverified";
    return (
      <AppSelect
        aria-label={label}
        value={state === "verified" ? "verified" : "unverified"}
        onChange={(event) => updateLiteral({ state: event.target.value })}
      >
        <option value="unverified">{t("databaseView:unverified")}</option>
        <option value="verified">{t("databaseView:verified")}</option>
      </AppSelect>
    );
  }
  if (property?.type === "place") {
    const place = literal && typeof literal === "object" && !Array.isArray(literal)
      ? literal as { lat?: unknown; lon?: unknown }
      : {};
    const updateCoordinate = (key: "lat" | "lon", raw: string) => {
      const parsed = raw === "" ? undefined : Number(raw);
      const lat = key === "lat" ? parsed : typeof place.lat === "number" ? place.lat : undefined;
      const lon = key === "lon" ? parsed : typeof place.lon === "number" ? place.lon : undefined;
      updateLiteral(lat === undefined && lon === undefined
        ? null
        : { ...(lat !== undefined ? { lat } : {}), ...(lon !== undefined ? { lon } : {}) });
    };
    return (
      <div className={styles.valueRow}>
        <input
          type="number"
          step="any"
          aria-label={t("databaseView:latitudeForAction", { number })}
          value={typeof place.lat === "number" ? place.lat : ""}
          onChange={(event) => updateCoordinate("lat", event.target.value)}
        />
        <input
          type="number"
          step="any"
          aria-label={t("databaseView:longitudeForAction", { number })}
          value={typeof place.lon === "number" ? place.lon : ""}
          onChange={(event) => updateCoordinate("lon", event.target.value)}
        />
      </div>
    );
  }
  if (words && property && AUTOMATION_WORDS_TYPES.has(property.type)) {
    return <AutomationWordsField words={automationWordsOf(action.value)} label={label} names={words.names} properties={words.properties}
      placeholder={!line ? t("databaseView:value") : property.type === "title" ? t("databaseView:untitled") : undefined} onFormula={words.onFormula} onChange={(text, spans) => onChange({ ...action, value: automationWordsValue(text, spans) })} />;
  }
  return <input aria-label={label} value={typeof literal === "string" ? literal : ""} onChange={(event) => updateLiteral(event.target.value)} />;
}

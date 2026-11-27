"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { usePageNames } from "@/lib/pageNames";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import type {
  DbProperty,
  PropertyConfig,
  RollupFunction,
} from "@/lib/types";
import { FormulaEditorDialog } from "./FormulaEditorDialog";
import { IdPrefixConfig } from "./IdPrefixConfig";
import { formulaNumberSettings } from "./formulaEditorModel";
import { Bell, FormulaIcon, NumbersIcon, PathRoundEndsIcon, Pencil, PersonCircleIcon, RelationIcon } from "../icons";
import { PropertySettingButton } from "./PropertySettingRow";
import { NotionSelect } from "./NotionSelect";
import { NumberPropertyConfig } from "./NumberPropertyConfig";
import { PERCENT_ROLLUPS, rollupNumberSettings } from "./rollupNumberDisplay";
import { PropertyDateConfig } from "./PropertyDateConfig";
import { PropertyRelationConfig } from "./PropertyRelationConfig";
import { RollupCalculationSelect } from "./RollupCalculationSelect";
import { statusGroupColumns, statusGroupLabelKey } from "./propertyStatusPolicy";
import { PropertySelectOptionsConfig } from "./PropertySelectOptionsConfig";
import { PropertyIcon } from "./PropertyIcon";
import { disambiguateOptionLabels, disambiguatePropertyLabels } from "./propertyOptionLabels";
import {
  canonicalRollupFunction,
  rollupFunctionsForPropertyType,
  rollupGroupCountOf,
  rollupShownFunction,
  rollupStatusGroupOf,
} from "./rollupPolicy";
import {
  propertyTypeConfigOperationContext,
  type PropertyTypeConfigContext,
  type PropertyTypeConfigSnapshot,
} from "./propertyTypeConfigTypes";
import styles from "./databaseSheet";
import { imeComposing } from "@/lib/fieldKeyBoundary";

const ROLLUP_UNLISTED_TYPES = new Set(["rollup", "button", "verification"]);
// A files property has none: Notion's column menu has no "속성 편집" for it
// and its settings no "제한" (seen 2026-11-07).
const CONFIGURED_TYPES = new Set(["select", "multi_select", "status", "relation", "number", "person", "rollup", "unique_id", "formula", "date",
  "created_time", "last_edited_time"]);

/** Whether a property of the type has settings for its "edit property" to show. */
export function propertyTypeHasConfig(type: string) {
  return CONFIGURED_TYPES.has(type);
}

/**
 * Type-specific configuration fields for a database property (relation database,
 * number format, rollup, formula). Self-contained so it can be reused by both the
 * table column header menu and the per-row property panel.
 */
export function PropertyTypeConfig({
  context,
  onClose,
  prop,
  snapshot,
  cellForm = false,
}: {
  context: PropertyTypeConfigContext;
  onClose?: () => void;
  prop: DbProperty;
  snapshot: PropertyTypeConfigSnapshot;
  /** Opened from an empty cell: a rollup's small form rather than its rows. */
  cellForm?: boolean;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation(["propertyTypeConfig", "propertyCell", "common"]);
  // The "수식 편집" row the formula editor hangs from while it is open.
  const [formulaRow, setFormulaRow] = useState<PopoverRect | null>(null);
  const formulaRowRef = useRef<HTMLButtonElement>(null);
  const { ops } = context;
  const operationContext = useMemo(
    () => propertyTypeConfigOperationContext(context, snapshot),
    [context.contextIdentity, snapshot.snapshotIdentity],
  );
  const sourceProps = [...(snapshot.propertiesByDatabase[prop.databaseId] ?? [])];
  // A relation's settings name the database it reaches, one in the trash too, as Notion's "관계형 대상" (seen
  // 2026-11-24 twice, the database's page in the trash); its mark locks the relation's pair (PropertyRelationConfig).
  const databases = snapshot.databases
    .filter((page) => page.kind === "database")
    .sort((a, b) => a.title.localeCompare(b.title) || a.position - b.position);

  // By name, as Notion's rollup lists them (seen 2026-11-02).
  const relationProps = sourceProps.filter((item) => item.type === "relation")
    .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
  const configuredRollupRelationId = prop.config?.rollupRelationPropertyId;
  const configuredRollupRelation = relationProps.find(
    (item) => item.id === configuredRollupRelationId,
  );
  const rollupRelationMissing = Boolean(
    configuredRollupRelationId && !configuredRollupRelation,
  );
  // Until a relation is chosen the rollup has none: its settings say
  // "선택", as Notion's (seen 2026-10-17), rather than showing the first
  // relation as chosen and saving it on opening.
  const rollupRelation = configuredRollupRelationId ? configuredRollupRelation : undefined;
  const rollupDatabaseId =
    rollupRelation?.config?.relationDatabaseId ?? rollupRelation?.databaseId;
  const rollupTargetProps = rollupDatabaseId
    ? [...(snapshot.propertiesByDatabase[rollupDatabaseId] ?? [])]
    : [];
  const relationTargetDatabaseId = prop.config?.relationDatabaseId ?? prop.databaseId;
  const relationTargetProps = snapshot.propertiesByDatabase[relationTargetDatabaseId] ?? [];
  const databaseOptions = databases.map((database) => ({
    value: database.id,
    label: pageNames.title(database),
    page: { icon: database.icon ?? "", iconType: database.iconType },
    inTrash: database.inTrash === true,
  }));
  const unavailableOption = {
    value: "",
    label: t("propertyCell:unavailable"),
  };
  const relationPropertyLabels = disambiguatePropertyLabels(
    relationProps,
    t("propertyTypeConfig:untitled"),
  );
  const relationPropertyOptions = [
    ...(rollupRelationMissing ? [unavailableOption] : []),
    ...relationProps.map((relationProp, index) => ({
      value: relationProp.id,
      label: relationPropertyLabels[index],
      icon: <PropertyIcon property={relationProp} size="small" />,
    })),
  ];
  // The target's title is the first choice, under its own name, and once
  // (2026-10-06: it showed as "이름 (1)" and again as "이름 (2)"). It is kept
  // as its property like any other: no property is a rollup not set up yet,
  // "선택", empty on every row as the server reads it. A rollup, button or
  // verification isn't listed, as Notion's (its rollup settings, seen
  // 2026-11-02); one already chosen stays, and is rolled up as before. The
  // rest follow by name (seen 2026-11-03).
  const rollupTargetTitle = rollupTargetProps.find((targetProp) => targetProp.type === "title");
  const rollupTargetPropertyBaseOptions = [
    {
      value: rollupTargetTitle?.id ?? "",
      label: rollupTargetTitle?.name || t("propertyTypeConfig:nameProperty"),
      ...(rollupTargetTitle ? { icon: <PropertyIcon property={rollupTargetTitle} size="small" /> } : {}),
    },
    ...rollupTargetProps.filter((targetProp) => targetProp.type !== "title"
      && (!ROLLUP_UNLISTED_TYPES.has(targetProp.type) || targetProp.id === prop.config?.rollupTargetPropertyId))
      .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0)).map((targetProp) => ({
      value: targetProp.id,
      label: targetProp.name || t("propertyTypeConfig:untitled"),
      icon: <PropertyIcon property={targetProp} size="small" />,
    })),
  ];
  const rollupTargetPropertyLabels = disambiguateOptionLabels(
    rollupTargetPropertyBaseOptions.map((option) => option.label),
  );
  const rollupTargetPropertyOptions = rollupRelationMissing
    ? [unavailableOption]
    : !rollupRelation ? []
    : rollupTargetPropertyBaseOptions.map((option, index) => ({
        ...option,
        label: rollupTargetPropertyLabels[index],
      }));
  const selectedRollupTarget = rollupTargetProps.find(
    (targetProp) => targetProp.id === prop.config?.rollupTargetPropertyId,
  );
  const compatibleRollupFunctions = rollupFunctionsForPropertyType(
    selectedRollupTarget?.type ?? "title",
  );
  // One counting per group without a status group shows, and reads, as
  // "원본 표시", as Notion's (seen 2026-11-12).
  const selectedRollupFunction = rollupShownFunction(prop.config) ?? "show_original";
  const rollupStatusGroup = rollupStatusGroupOf(prop.config);
  // A status target's groups, which a count or share per group takes.
  const rollupGroup = rollupStatusGroup ?? rollupGroupCountOf(prop.config);
  const rollupStatusGroups = selectedRollupTarget?.type === "status" ? statusGroupColumns(selectedRollupTarget.config).map((group) => ({
    id: group.id, label: statusGroupLabelKey(group.name) ? t(`propertyCell:${statusGroupLabelKey(group.name)}`) : group.name,
  })) : undefined;
  const rollupCalculationConfig = (fn: string, group?: string) => ({
    ...prop.config, rollupRelationPropertyId: rollupRelation?.id, rollupFunction: fn as RollupFunction, rollupStatusGroup: group,
  });
  const selectedRollupFunctionUnsupported = !rollupGroup && !compatibleRollupFunctions.includes(
    selectedRollupFunction,
  );
  const rollupNumbers = rollupRelation
    ? rollupNumberSettings(selectedRollupFunction, selectedRollupTarget, prop.config) : null;
  // A rollup a feature keeps, as a Sprints database's two: its three rows are
  // dimmed and fixed and only how its numbers show changes, as Notion's (seen 2026-11-04).
  const rollupLocked = prop.config?.rollupLocked === true;

  useEffect(() => {
    if (rollupDatabaseId) {
      void ops.loadDatabase(operationContext, rollupDatabaseId, { rows: false });
    }
  }, [
    operationContext.contextIdentity,
    operationContext.snapshotIdentity,
    ops,
    rollupDatabaseId,
  ]);

  function updateProperty(id: string, patch: Partial<DbProperty>) {
    void ops.updateProperty(operationContext, id, patch);
  }

  // Its target is the related database's title at once, as Notion's (seen
  // 2026-11-02 from both the column's settings and an empty cell's).
  function onRollupRelationChange(value: string) {
    const relation = relationProps.find((item) => item.id === value);
    const relatedId = relation?.config?.relationDatabaseId ?? relation?.databaseId;
    const title = relatedId ? snapshot.propertiesByDatabase[relatedId]?.find((item) => item.type === "title") : undefined;
    const currentFunction = canonicalRollupFunction(prop.config?.rollupFunction) ?? "show_original";
    updateProperty(prop.id, {
      config: {
        ...prop.config,
        rollupRelationPropertyId: value,
        rollupTargetPropertyId: title?.id,
        rollupFunction: !title || rollupFunctionsForPropertyType("title").includes(currentFunction)
          ? prop.config?.rollupFunction ?? "show_original" : "show_original",
      },
    });
  }

  function onRollupTargetChange(value: string) {
    const nextTarget = rollupTargetProps.find((targetProp) => targetProp.id === value);
    const currentFunction =
      canonicalRollupFunction(prop.config?.rollupFunction) ?? "show_original";
    const nextFunctions = rollupFunctionsForPropertyType(nextTarget?.type ?? "title");
    updateProperty(prop.id, {
      config: {
        ...prop.config,
        rollupRelationPropertyId: rollupRelation?.id,
        rollupTargetPropertyId: value || undefined,
        rollupFunction: nextFunctions.includes(currentFunction)
          ? currentFunction
          : "show_original",
      },
    });
  }

  function onFieldKeyDown(e: React.KeyboardEvent) {
    if (imeComposing(e)) return;
    if (e.key !== "Escape") return;
    e.preventDefault();
    onClose?.();
  }

  if (prop.type === "select" || prop.type === "multi_select" || prop.type === "status") {
    return (
      <PropertySelectOptionsConfig
        operationContext={operationContext}
        ops={ops}
        prop={prop}
      />
    );
  }

  if (prop.type === "relation") {
    return (
      <PropertyRelationConfig
        prop={prop}
        databaseOptions={databaseOptions}
        targetProps={relationTargetProps}
        operationContext={operationContext}
        ops={ops}
      />
    );
  }

  if (prop.type === "date" || prop.type === "created_time" || prop.type === "last_edited_time") {
    return <PropertyDateConfig prop={prop} reminder={prop.type === "date"} onConfig={(config) => updateProperty(prop.id, { config })} />;
  }

  if (prop.type === "number") {
    return <NumberPropertyConfig prop={prop} onConfig={(config) => updateProperty(prop.id, { config })} />;
  }

  if (prop.type === "person") {
    const notifyOptions = (["users_and_groups", "users", "none"] as const).map((value) => ({
      value,
      label: t(`propertyTypeConfig:notifyOptions.${value}`),
    }));
    // As Notion's (seen 2026-10-10): 제한, 기본값 and 알림, a row each.
    const setPersonConfig = (patch: Pick<PropertyConfig, "limit" | "defaultCreator">) => {
      const next: PropertyConfig = { ...prop.config, ...patch };
      if (next.limit !== 1) delete next.limit;
      if (!next.defaultCreator) delete next.defaultCreator;
      updateProperty(prop.id, { config: next });
    };
    return (
      <div className={styles.propertyHeaderFieldset}>
        <NotionSelect
          ariaLabel={t("propertyTypeConfig:relationLimit")} settingLabel={t("propertyTypeConfig:relationLimit")} settingIcon={<NumbersIcon size="regular" />}
          value={prop.config?.limit === 1 ? "one" : "none"}
          options={[
            { value: "one", label: t("propertyTypeConfig:personLimitOne") },
            { value: "none", label: t("propertyTypeConfig:relationLimitNone") },
          ]}
          onChange={(value) => setPersonConfig({ limit: value === "one" ? 1 : undefined })}
        />
        <NotionSelect
          ariaLabel={t("propertyTypeConfig:personDefault")} settingLabel={t("propertyTypeConfig:personDefault")} settingIcon={<PersonCircleIcon size="small" />}
          value={prop.config?.defaultCreator ? "creator" : "none"}
          options={[
            { value: "none", label: t("propertyTypeConfig:personDefaultNone") },
            { value: "creator", label: t("propertyTypeConfig:personDefaultCreator") },
          ]}
          onChange={(value) => setPersonConfig({ defaultCreator: value === "creator" || undefined })}
        />
        <NotionSelect
          ariaLabel={t("propertyTypeConfig:notify")} settingLabel={t("propertyTypeConfig:notify")} settingIcon={<Bell size="small" />}
          value={prop.config?.notify ?? "users"}
          options={notifyOptions}
          onChange={(value) =>
            updateProperty(prop.id, {
              config: { ...prop.config, notify: value as NonNullable<PropertyConfig["notify"]> },
            })
          }
        />
      </div>
    );
  }

  // A rollup's rows, as Notion's (seen 2026-10-10): ↗ relation, a path from a dot to a ring the target
  // property ("대상 속성", seen 2026-11-19), Σ the calculation, each named by its own words (seen 2026-11-19).
  // An empty cell's rollup form, as Notion's (seen 2026-11-03): "관계형",
  // "속성" and "계산" over their choices, the two after it faint until a
  // relation is chosen.
  // A rollup a feature keeps shows its three without their "⌄" (Notion's Sprints, seen 2026-11-06).
  if (prop.type === "rollup" && cellForm && relationProps.length > 0) {
    const calculation = { value: selectedRollupFunction, targetType: selectedRollupTarget?.type ?? "title",
      statusGroups: rollupStatusGroups, group: rollupGroup,
      unavailable: selectedRollupFunctionUnsupported, disabled: !rollupRelation || rollupLocked };
    return (
      <div className={styles.rollupForm} data-rollup-form data-locked={rollupLocked || undefined}>
        <section>
          <span>{t("propertyTypeConfig:relation")}</span>
          <NotionSelect ariaLabel={t("propertyTypeConfig:relation")} value={rollupRelationMissing ? "" : rollupRelation?.id ?? ""}
            options={relationPropertyOptions} placeholder={t("propertyTypeConfig:rollupFormRelation")} disabled={rollupLocked}
            className={styles.rollupFormSelect} buttonClassName={styles.rollupFormChoice} onChange={onRollupRelationChange} />
        </section>
        <section data-faint={!rollupRelation || undefined} data-rollup-form-target>
          <span>{t("propertyTypeConfig:rollupFormProperty")}</span>
          <NotionSelect ariaLabel={t("propertyTypeConfig:rollupFormProperty")} value={prop.config?.rollupTargetPropertyId ?? ""}
            options={rollupTargetPropertyOptions} placeholder={t("propertyTypeConfig:rollupFormChooseProperty")} disabled={!rollupRelation || rollupLocked}
            className={styles.rollupFormSelect} buttonClassName={styles.rollupFormChoice} onChange={onRollupTargetChange} />
        </section>
        <section data-faint={!rollupRelation || undefined}>
          <span>{t("propertyTypeConfig:calculate")}</span>
          <RollupCalculationSelect label={t("propertyTypeConfig:calculate")} ariaLabel={t("propertyTypeConfig:calculate")}
            {...calculation} plainClassName={styles.rollupFormChoice}
            onChange={(fn, group) => updateProperty(prop.id, { config: rollupCalculationConfig(fn, group) })} />
        </section>
      </div>
    );
  }

  if (prop.type === "rollup") {
    return (
      <div className={styles.propertyHeaderFieldset}>
        {rollupRelationMissing && (
          <div
            role="alert"
            className={styles.toolbarEmpty}
            data-rollup-broken="relation"
          >
            {t("propertyCell:unavailable")}: {t("propertyTypeConfig:relation")} /{" "}
            {t("propertyTypeConfig:property")}
          </div>
        )}
        {relationProps.length === 0 && !rollupRelationMissing ? (
          <div className={styles.toolbarEmpty}>{t("propertyTypeConfig:addRelationFirst")}</div>
        ) : (
          <>
            <div>
              <NotionSelect
                ariaLabel={t("propertyTypeConfig:relation")} settingLabel={t("propertyTypeConfig:relation")} settingIcon={<RelationIcon size="small" />}
                value={rollupRelationMissing ? "" : rollupRelation?.id ?? ""}
                options={relationPropertyOptions} placeholder={t("propertyTypeConfig:choose")}
                disabled={rollupLocked}
                onChange={onRollupRelationChange}
              />
            </div>
            {/* Its two rows after the relation, dim until one is chosen, have their names in grey (PropertySettingRow.module.css). */}
            <div data-setting-name-faint={!rollupRelation || undefined}>
              <NotionSelect
                ariaLabel={t("propertyTypeConfig:property")} settingLabel={t("propertyTypeConfig:property")} settingIcon={<PathRoundEndsIcon size="small" />}
                value={rollupRelationMissing ? "" : prop.config?.rollupTargetPropertyId ?? ""}
                options={rollupTargetPropertyOptions} placeholder={t("propertyTypeConfig:choose")}
                disabled={!rollupRelation || rollupLocked}
                onChange={onRollupTargetChange}
              />
            </div>
            <div data-setting-name-faint={!rollupRelation || undefined}>
              <RollupCalculationSelect
                label={t("propertyTypeConfig:calculate")} ariaLabel={t("propertyTypeConfig:calculate")}
                icon={<FormulaIcon size="small" />}
                value={selectedRollupFunction} targetType={selectedRollupTarget?.type ?? "title"}
                statusGroups={rollupStatusGroups} group={rollupGroup}
                unavailable={selectedRollupFunctionUnsupported} disabled={!rollupRelation || rollupLocked} unset={!rollupRelation}
                onChange={(fn, group) => updateProperty(prop.id, { config: rollupCalculationConfig(fn, group) })}
              />
            </div>
          </>
        )}
        {rollupNumbers && (
          <NumberPropertyConfig prop={prop} onConfig={(config) => updateProperty(prop.id, { config })}
            shows={rollupNumbers === "format" ? "all" : PERCENT_ROLLUPS.has(selectedRollupFunction) || rollupStatusGroup ? "percent display" : "display"} />
        )}
      </div>
    );
  }

  if (prop.type === "unique_id") {
    return <IdPrefixConfig key={prop.id} dataSourceId={prop.databaseId} propertyId={prop.id} prefix={prop.config?.idPrefix ?? ""} onKeyDown={onFieldKeyDown}
      databaseTitle={snapshot.databases.find((database) => database.id === prop.databaseId)?.title}
      onUpdate={(idPrefix) => updateProperty(prop.id, { config: { ...prop.config, idPrefix } })} />;
  }

  if (prop.type === "formula") {
    // "수식 편집" opens the formula editor its cells open too, hung under
    // its row with a ∨ while open (FormulaEditorDialog `fromRow`); a number it
    // works out takes a format, decimal places and a number, bar or ring, as
    // Notion's formula settings (seen 2026-10-05), a list of numbers the same
    // without decimal places, and any other result nothing more (2026-11-07).
    const formulaConfig = (prop.config ?? {}) as Record<string, unknown>;
    const numberSettings = formulaNumberSettings(String(formulaConfig.formula ?? formulaConfig.expression ?? ""), sourceProps);
    return (
      <>
      <div>
        <PropertySettingButton label={t("propertyTypeConfig:editFormula")} icon={<Pencil size="small" />} buttonRef={formulaRowRef}
          aria-label={t("propertyTypeConfig:editFormula")} aria-expanded={formulaRow !== null} data-edit-formula
          onClick={() => setFormulaRow(popoverRectOf(formulaRowRef.current))} />
        {formulaRow ? (
          <FormulaEditorDialog
            anchor={formulaRow}
            fromRow
            prop={prop}
            props={sourceProps}
            pagesById={snapshot.pagesById ?? {}}
            propsByDb={snapshot.propertiesByDatabase as Record<string, DbProperty[]>}
            bounds={snapshot.formulaBounds}
            onSave={(formula) => updateProperty(prop.id, { config: { ...prop.config, formula } })}
            onClose={() => setFormulaRow(null)}
          />
        ) : null}
      </div>
      {numberSettings ? <NumberPropertyConfig prop={prop} shows={numberSettings}
        onConfig={(config) => updateProperty(prop.id, { config })} /> : null}
      </>
    );
  }

  return null;
}

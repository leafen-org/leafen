"use client";

import type { PageIconSource } from "@/components/page/PageIcon";
import { SwitchInput } from "@/components/Switch";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { DbProperty } from "@/lib/types";
import { ArrowUpRightIcon, LinkedViewIcon } from "@/icons/leafen";
import { ArrowsExpandHorizontal, NumbersIcon } from "../icons";
import { DataSourceMark } from "./DataSourceMark";
import { PropertySettingButton, PropertySettingPopover } from "./PropertySettingRow";
import numberStyles from "./NumberPropertyConfig.module.css";
import { NotionSelect } from "./NotionSelect";
import type {
  PropertyTypeConfigOperationContext,
  PropertyTypeConfigOps,
} from "./propertyTypeConfigTypes";
import styles from "./databaseSheet";
import { sprintManagedProperty } from "./databaseSprintContext";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";

/**
 * Relation config: the related database, chosen when the relation is made
 * and shown dimmed after, as Notion's (seen 2026-10-10); limit it to one page or none
 * (Notion's "제한"), and optionally add the related property (Notion's
 * "관련된 속성 추가하기"), a relation on the related database the server
 * creates and keeps in step.
 */
export function PropertyRelationConfig({
  databaseOptions,
  operationContext,
  ops,
  prop,
  targetProps,
}: {
  /** The databases it may name, one in the trash marked `inTrash` (dataSourcePage). */
  databaseOptions: { value: string; label: string; page?: PageIconSource; inTrash?: boolean }[];
  operationContext: PropertyTypeConfigOperationContext;
  ops: PropertyTypeConfigOps;
  prop: DbProperty;
  targetProps: readonly DbProperty[];
}) {
  const { t } = useTranslation(["propertyTypeConfig", "propertyCell", "common"]);
  const targetDbId = prop.config?.relationDatabaseId ?? prop.databaseId;
  // A relation to its own database made here relates both rows on its own
  // (relationSymmetric), as Notion's; shown two-way it keeps a second
  // property on the same database in step.
  const linkedId = prop.config?.relatedPropertyId;
  const twoWay = Boolean(linkedId);
  const sprintRelation = sprintManagedProperty(prop);
  const reciprocal = linkedId
    ? targetProps.find((item) => item.id === linkedId)
    : undefined;

  const [reciprocalName, setReciprocalName] = useState(reciprocal?.name ?? "");
  useEffect(() => {
    setReciprocalName(reciprocal?.name ?? "");
  }, [reciprocal?.id, reciprocal?.name]);

  // Load the target schema so the reciprocal property name renders when the
  // related database has not been opened yet.
  useEffect(() => {
    if (twoWay) void ops.loadDatabase(operationContext, targetDbId, { rows: false });
  }, [
    operationContext.contextIdentity,
    operationContext.snapshotIdentity,
    ops,
    targetDbId,
    twoWay,
  ]);

  // A relation limited to one page keeps the pages it has; its cell replaces
  // them with the next one picked.
  function setLimit(value: string) {
    const { limit: _limit, ...rest } = prop.config ?? {};
    void ops.updateProperty(operationContext, prop.id, {
      config: value === "one" ? { ...rest, limit: 1 } : rest,
    });
  }

  // The server names the paired property, which may live on another page.
  function commitReciprocalName() {
    const trimmed = reciprocalName.trim();
    if (!trimmed || trimmed === reciprocal?.name) {
      setReciprocalName(reciprocal?.name ?? "");
      return;
    }
    void ops.setRelationTwoWay(operationContext, prop.id, true, trimmed);
  }

  // A relation to its own database reads "이 데이터베이스", as Notion's (seen 2026-10-11).
  const self = Boolean(targetDbId) && targetDbId === prop.databaseId;
  const targetOption = databaseOptions.find((option) => option.value === targetDbId);
  const targetName = self ? t("propertyTypeConfig:thisDatabaseSuffix") : targetOption?.label ?? t("propertyCell:unavailable");
  // The related property's row: "↔ 양방향 관계형" to another database, "↗ 관련된 속성
  // 추가하기" to its own, each with its own ⓘ, its value "이 데이터베이스" with no icon
  // before it and the target's row a table with an arrow either way, as Notion's
  // (seen 2026-11-09, 2026-11-14).
  const pairLabel = t(self ? "propertyTypeConfig:twoWayRelation" : "propertyTypeConfig:twoWayRelationSetting");
  const pairIcon = self ? <ArrowUpRightIcon size="small" /> : <ArrowsExpandHorizontal size="small" />;
  // While the database it reaches is in the trash it keeps its pair as it is, its row dimmed with no "›", as
  // Notion's (seen 2026-11-24 in two tabs: "양방향 관계형 꺼짐" disabled with the target's page in the trash, and
  // open again once the page came back); "관계형 대상" names it still.
  const pairLocked = !self && targetOption?.inTrash === true;
  const pairTip = t(self ? "propertyTypeConfig:relatedPropertyTip" : "propertyTypeConfig:twoWayRelationTip");
  return (
    <div className={styles.propertyHeaderFieldset}>
      <div>
        <PropertySettingButton label={t("propertyTypeConfig:relationTarget")} icon={<LinkedViewIcon size="small" />}
          // The target by its data source's mark, its emoji or else the cylinder in a 20px box, as Notion's (seen
          // 2026-11-16: "🍎" 18px, a cylinder 13.8×15.3), a link to it whose name stands 6px after the mark (data-value-link).
          valueIcon={self ? undefined : <DataSourceMark source={targetOption?.page ?? {}} />}
          disabled value={targetName} data-value-link="" />
      </div>
      <div>
        <NotionSelect
          ariaLabel={t("propertyTypeConfig:relationLimit")} settingLabel={t("propertyTypeConfig:relationLimit")} settingIcon={<NumbersIcon size="regular" />}
          value={prop.config?.limit === 1 ? "one" : "none"}
          options={[
            { value: "one", label: t("propertyTypeConfig:relationLimitOne") },
            { value: "none", label: t("propertyTypeConfig:relationLimitNone") },
          ]}
          onChange={setLimit}
        />
      </div>
      {/* A relation its sprints manage — a task's "스프린트", a sprint's "작업" — stays two-way, its row dimmed and its
          ⓘ saying the other database is not this one's to change, as Notion's (seen 2026-11-07, 2026-11-08). */}
      {sprintRelation ? (
        <div>
          <PropertySettingButton label={t("propertyTypeConfig:twoWayRelationSetting")}
            labelTip={t("propertyTypeConfig:twoWayRelationLockedTip", { name: targetName })}
            icon={<ArrowsExpandHorizontal size="small" />} value={t("propertyTypeConfig:settingOn")} disabled />
        </div>
      ) : pairLocked ? (
        <div>
          <PropertySettingButton label={pairLabel} labelTip={pairTip} icon={pairIcon}
            value={t(twoWay ? "propertyTypeConfig:settingOn" : "propertyTypeConfig:settingOff")} disabled />
        </div>
      ) : <PropertySettingPopover label={pairLabel} labelTip={pairTip}
        icon={pairIcon}
        value={t(twoWay ? "propertyTypeConfig:settingOn" : "propertyTypeConfig:settingOff")}>
        <label className={numberStyles.switchRow}>
          <span>{t("propertyTypeConfig:activateRelation")}</span>
          <SwitchInput checked={twoWay}
            aria-label={pairLabel}
            onChange={e => void ops.setRelationTwoWay(operationContext, prop.id, e.target.checked)} />
        </label>
        <label className={styles.propertyHeaderField}>
          <span>{t("propertyTypeConfig:relatedPropertyName")}</span>
          <input
            autoFocus
            disabled={!twoWay}
            placeholder={databaseOptions.find(option => option.value === prop.databaseId)?.label}
            value={reciprocalName}
            aria-label={t("propertyTypeConfig:relatedPropertyName")}
            onFocus={(e) => e.currentTarget.select()}
            onChange={(e) => setReciprocalName(e.target.value)}
            onBlur={commitReciprocalName}
            onKeyDown={(e) => {
              if (imeComposing(e)) return;
              if (fieldEnter(e)) {
                e.preventDefault();
                e.currentTarget.blur();
              }
            }}
          />
        </label>
      </PropertySettingPopover>}
    </div>
  );
}

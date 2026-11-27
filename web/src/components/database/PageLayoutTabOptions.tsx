import { useState, type KeyboardEvent } from "react";
import { useTranslation } from "react-i18next";
import { ChevronRight } from "@/icons/leafen";
import { DataSourceMark } from "./DataSourceMark";
import type { RelationDatabaseOption } from "@/lib/relationPropertyCreationModel";
import { SwitchMark } from "@/components/Switch";
import { SettingInfoTip } from "../IconTipButton";
import { ArrowsExpandHorizontal } from "../icons";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { plainTitleOf } from "@/lib/closedMentions";
import { dataSourceDisplayTitle } from "../editor/databaseTitles";
import styles from "./PageLayoutAddMenu.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** What a new tab is named, and the related property's name of a relation made with it. */
export type PageLayoutTabNames = { name: string; relatedName?: string };

/**
 * A source the tab menu lists, by its name or, untitled, "새 데이터베이스" as
 * its database's only one and "새 데이터 소스" as one of several
 * (dataSourceDisplayTitle), as Notion's (seen 2026-11-18).
 */
export function tabSourceName(source: RelationDatabaseOption) {
  return dataSourceDisplayTitle(plainTitleOf(source), source.sourceTitles?.length ?? 1);
}

/**
 * The tab menu's "보기 옵션" for a chosen data source, as Notion's (seen
 * 2026-11-12 at 1440): the view's name in a field, the source's name in it
 * and the keys there — an untitled source's empty under "보기 이름", and
 * "보기 추가" held while the name is empty, as for a name emptied (seen
 * 2026-11-18); "보기 옵션 ⊞ 〈source〉 ›" ("View of" in English) back
 * to the sources — "이 데이터베이스 ›" for the database's own, as Notion's
 * (seen 2026-11-12);
 * "새 관계형 추가" with its switch, which on opens under a line the
 * two-way relation, on and fixed, and "관계형 속성 이름" — the related
 * property's name, this database's by default — the blue button then
 * "보기 및 관계형 추가": a relation to the source named as the tab, and the
 * tab of its pages.
 */
export function PageLayoutTabOptions({ source, databaseTitle, own = false, canRelate, onBack, onAdd }: {
  source: RelationDatabaseOption;
  databaseTitle: string;
  /** The source is the database's own. */
  own?: boolean;
  canRelate: boolean;
  onBack(): void;
  /** The tab, with a relation made for it when `relate`; it may be refused (the relation not made). */
  onAdd(names: PageLayoutTabNames, relate: boolean): Promise<void> | void;
}) {
  const { t } = useTranslation(["pageLayout", "propertyTypeConfig", "databaseView"]);
  const sourceName = tabSourceName(source);
  const [name, setName] = useState(source.untitled ? "" : plainTitleOf(source));
  const [relate, setRelate] = useState(false);
  const [relatedName, setRelatedName] = useState("");
  const [busy, setBusy] = useState(false);
  const relatedDefault = databaseTitle || untitledPageDisplayTitle("database");
  // "보기 이름", or "보기 및 관계형 이름" with a relation made for it (Notion's, seen 2026-11-12).
  const nameLabel = relate ? t("viewAndRelationName") : t("databaseView:viewName");
  const named = Boolean(name.trim());
  async function add() {
    if (busy || !named) return;
    setBusy(true);
    try {
      await onAdd({ name: name.trim(), ...(relate ? { relatedName: relatedName.trim() || relatedDefault } : {}) }, relate);
    } finally {
      setBusy(false);
    }
  }
  const enterAdds = (event: KeyboardEvent<HTMLInputElement>) => {
    if (!fieldEnter(event)) return;
    event.preventDefault();
    void add();
  };
  return (
    <div className={styles.tabOptions} data-layout-tab-options>
      <input className={styles.tabOptionsName} value={name} placeholder={nameLabel} aria-label={nameLabel}
        autoFocus onFocus={(event) => event.currentTarget.setSelectionRange(event.currentTarget.value.length, event.currentTarget.value.length)}
        disabled={busy} onChange={(event) => setName(event.target.value)} onKeyDown={enterAdds} data-layout-tab-name />
      <button type="button" className={styles.row} onClick={onBack} disabled={busy} data-layout-tab-options-source>
        <span className={styles.text}><span>{t("viewOfSource")}</span></span>
        <span className={styles.tabOptionsSource}>
          {own ? <span>{t("pageLayout:thisDatabase")}</span> : <><DataSourceMark source={source.mark ?? {}} /><span>{sourceName}</span></>}
        </span>
        <ChevronRight size="small" aria-hidden="true" className={styles.tabOptionsChevron} />
      </button>
      {canRelate ? (
        <button type="button" role="switch" aria-checked={relate} className={styles.row} disabled={busy}
          onClick={() => setRelate((on) => !on)} data-layout-tab-new-relation>
          <span className={styles.text}><span>{t("newRelation")}</span></span>
          <SwitchMark on={relate} className={styles.tabOptionsSwitch} />
        </button>
      ) : null}
      {relate ? (
        <>
          <div className={styles.divider} role="separator" />
          {/* A tab's relation is two-way, the switch on and fixed. */}
          <div className={`${styles.row} ${styles.tabOptionsFixed}`} role="switch" aria-checked="true" aria-disabled="true">
            <ArrowsExpandHorizontal size="regular" aria-hidden="true" className={styles.tabOptionsIcon} />
            <span className={styles.tabOptionsLabel} data-setting-name>
              {t("propertyTypeConfig:twoWayRelationSetting")}
              <SettingInfoTip label={t("propertyTypeConfig:twoWayRelationTip")} />
            </span>
            <SwitchMark on className={styles.tabOptionsSwitch} />
          </div>
          <label className={styles.tabOptionsRelated}>
            <span>{t("propertyTypeConfig:relatedPropertyName")}</span>
            <input className={styles.tabOptionsName} value={relatedName} placeholder={relatedDefault} autoFocus disabled={busy}
              onChange={(event) => setRelatedName(event.target.value)} onKeyDown={enterAdds} data-layout-tab-related-name />
          </label>
        </>
      ) : null}
      <button type="button" className={styles.tabOptionsAdd} disabled={busy || !named} onClick={() => void add()} data-layout-tab-add-view>
        {relate ? t("addViewAndRelation") : t("addView")}
      </button>
    </div>
  );
}

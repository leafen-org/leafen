import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronLeft, X } from "@/icons/leafen";
import { databaseStarterLabel as label, databaseStarterProperties, defaultDatabaseStarterSelection,
  databaseStarterPreviewRows, databaseStarterPreviewView,
  type DatabaseStarterAuthor, type DatabaseStarterFeature, type DatabaseStarterSelection, type DatabaseStarterTemplate } from "@/lib/databaseStarterCatalog";
import { BuiltInIconGlyph } from "@/components/page/PageIcon";
import { parseNativeIconValue } from "@/components/page/nativeIcons";
import { iconPixels } from "@/icons/leafen";
import { PropertyTypeIcon } from "./PropertyTypeIcon";
import { ViewIcon } from "./databaseViewShared";
import { DatabaseStarterPreview, StarterTemplateIcon } from "./DatabaseStarterPreview";
import { tableColumnSettings } from "./tablePropertyMenuModel";
import { projectVisibleProperties } from "./tableViewModel";
import styles from "./DatabaseStarterGallery.module.css";

export function DatabaseStarterCustomizer({ template, busy, failed, previewAuthor, fromPanel = false, onCancel, onApply }: {
  template: DatabaseStarterTemplate; busy: boolean; failed: boolean; onCancel(): void;
  previewAuthor?: DatabaseStarterAuthor;
  /**
   * Opened from the new database panel: its features step has a corner ×
   * and "시작하기" (the gallery's, "←" and "계속"); either goes on to the views
   * and "완료", which makes it, as Notion's (seen 2026-11-11 for task
   * tracker, projects and docs hub from the panel, brainstorm from the gallery).
   */
  fromPanel?: boolean;
  onApply(selection: DatabaseStarterSelection): void;
}) {
  const { t } = useTranslation(["sidebar", "common", "editor"]);
  const [selection, setSelection] = useState(() => defaultDatabaseStarterSelection(template));
  const [step, setStep] = useState(0);
  const [previewTime] = useState(() => new Date().toISOString());
  const backRef = useRef<HTMLButtonElement>(null);
  // The corner × takes the focus. Measured 2026-11-15 after the press only (no script in the page before it), Notion in Chrome 1440: "작업 트래커" from the
  // new database panel, × focused with its ring at once, "z" went nowhere (Leafen: × focused, its ring
  // only after the key — seen once).
  useEffect(() => { backRef.current?.focus({ preventScroll: true }); }, []);
  const [previewId, setPreviewId] = useState(template.views[0]?.id);
  const views = template.views.filter(v => selection.views.includes(template.viewChoices.find(c => c.id === v.id)?.key ?? ""));
  const view = views.find(v => v.id === previewId) ?? views[0];
  const properties = databaseStarterProperties(template, selection.features, view?.id);
  const previewView = databaseStarterPreviewView(view, properties, template);
  const rows = databaseStarterPreviewRows(template, selection.features).map(row => ({ ...row, createdAt: previewTime, updatedAt: previewTime,
    createdBy: previewAuthor?.id, lastEditedBy: previewAuthor?.id }));
  // What the view hides, its grouping property apart: a board by status showing its name, assignee and priority
  // says "숨겨진 속성 4개" of its eight, as Notion's (seen 2026-11-11).
  const groupedBy = typeof previewView?.config.groupBy === "string" ? previewView.config.groupBy : undefined;
  const shownIds = new Set(projectVisibleProperties({ properties, ...tableColumnSettings(previewView?.config ?? {}) }).map(p => p.id));
  const hiddenCount = properties.filter(p => !shownIds.has(p.id) && p.id !== groupedBy).length;
  const choices = step === 0 ? template.features : template.viewChoices;
  const closes = fromPanel && step === 0;
  const chosen = step === 0 ? selection.features : selection.views;
  function toggle(key: string) {
    const field = step === 0 ? "features" : "views";
    setSelection(current => step === 1 && current.views.length === 1 && current.views.includes(key) ? current
      : ({ ...current, [field]: current[field].includes(key) ? current[field].filter(k => k !== key) : [...current[field], key] }));
  }
  return <div className={styles.customizer} data-database-starter-customizer={template.key}>
    <div className={styles.customizerLeft}>
      <button type="button" ref={backRef} className={styles.back} aria-label={t(closes ? "common:actions.close" : "sidebar:addWindow.back")} disabled={busy}
        onClick={() => step ? setStep(0) : onCancel()}>{closes ? <X size="small" /> : <ChevronLeft size="small" />}</button>
      <h2>{t("sidebar:addWindow.customize", { title: label(template.name) })}</h2>
      <p>{t(step ? "sidebar:addWindow.chooseViews" : "sidebar:addWindow.chooseFeatures", { title: label(template.name) })}</p>
      <div className={styles.chips} data-view-choices={step === 1 || undefined}>{choices.map(choice => <button key={choice.key} type="button" aria-pressed={chosen.includes(choice.key)}
        disabled={busy} onClick={() => toggle(choice.key)} data-starter-choice={choice.key}>
        {step ? <ViewChipIcon view={template.views.find(v => v.id === (choice as {id?: string}).id) ?? { type: "table" }} chosen={chosen.includes(choice.key)} />
          : <FeatureIcon template={template} feature={choice as DatabaseStarterFeature} chosen={chosen.includes(choice.key)} />}{label(choice.name)}
      </button>)}</div>
      <div className={styles.customizerFooter}>
        {failed ? <p role="alert">{t("sidebar:addWindow.createFailed")}</p> : null}
        <button type="button" className={styles.continue} disabled={busy || (step === 1 && !selection.views.length)}
          onClick={() => step ? onApply(selection) : setStep(1)}>
          {busy ? t("editor:starter.creating") : t(step ? "common:actions.done" : fromPanel ? "sidebar:addWindow.start" : "sidebar:addWindow.continue")}</button>
      </div>
    </div>
    <div className={styles.customizerRight}>
      <strong className={styles.previewHeading}><StarterTemplateIcon icon={template.icon} size={28} />{label(template.name)}</strong>
      <div className={styles.previewTabs} role="tablist" aria-label={t("sidebar:addWindow.preview")}>{views.map(v => <button key={v.id} role="tab" type="button"
        aria-selected={view?.id === v.id} onClick={() => setPreviewId(v.id)}><ViewIcon view={v} />{label(v.name)}</button>)}</div>
      <DatabaseStarterPreview title={label(template.name)} icon={template.icon} properties={properties} rows={rows} view={previewView} heading={false} author={previewAuthor} />
      {hiddenCount > 0 ? <span className={styles.hiddenProperties}>{t("sidebar:addWindow.hiddenProperties", {count:hiddenCount})}</span> : null}
    </div>
  </div>;
}

/**
 * A view's chip icon: its own drawn blue on a chosen chip and in its own
 * colour on another, as Notion's (seen 2026-11-11: "상태별"'s arrow blue,
 * "내 프로젝트"'s person grey, turning grey as its chip is left); a layout's
 * mark takes the chip's (DatabaseStarterGallery.module.css). Notion's place
 * is 20px, drawn 1.2 times over (seen 2026-11-24).
 */
function ViewChipIcon({ view, chosen }: { view: { type: string; config?: Record<string, unknown> | null }; chosen: boolean }) {
  const own = typeof view.config?.icon === "string" ? parseNativeIconValue(view.config.icon.trim()) : undefined;
  return own ? <BuiltInIconGlyph icon={own} size={iconPixels("regular")} color={chosen ? "var(--accent-blue)" : undefined} /> : <ViewIcon view={view} size="regular" />;
}

/**
 * A feature's chip icon: its own (progress's chart, budget's coins, as Notion's, seen 2026-11-11), else its first
 * property's type; in the secondary icon colour (icoSec), blue on a chosen chip (seen 2026-11-24).
 */
function FeatureIcon({ template, feature, chosen }: { template: DatabaseStarterTemplate; feature: DatabaseStarterFeature; chosen: boolean }) {
  if (feature.icon) return <BuiltInIconGlyph icon={{ name: feature.icon, color: "gray" }} size={16} color={chosen ? "var(--accent-blue)" : "var(--icon-secondary)"} />;
  return <PropertyTypeIcon type={template.properties.find(p => p.id === feature.properties[0])?.type ?? "rich_text"} size="small" />;
}

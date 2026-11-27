import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { appText } from "@/components/admin/administratorAppsApi";
import { useUsableAppList } from "@/components/apps/useUsableApps";
import { useProductPages } from "@/components/product/productPagesContext";
import { CheckIcon, ChevronRight, ServerAppsIcon } from "@/icons/leafen";
import styles from "./databaseSheet";

/** The apps' editors that take a property's type, and the one it has chosen. */
export function usePropertyAppEditors(property: GraphProperty) {
  const pages = useProductPages();
  const organizationId = pages?.organizationId;
  const { apps } = useUsableAppList(pages?.session, Boolean(organizationId));
  const current = property.config.appEditor as { app?: string; editor?: string } | undefined;
  const editors = apps.filter((app) => app.tenancyKey === `org:${organizationId}`).flatMap((app) => (app.propertyEditors ?? [])
    .filter((editor) => editor.types.includes(property.type)).map((editor) => ({ app, editor })));
  return { editors, current };
}

/**
 * "표시 방식" in a property's settings (docs/app-platform.md 10-1):
 * Leafen's own, or an editor of an app the person may use in the page's
 * organization that takes the property's type — named with Leafen's "앱"
 * mark and the app's name. Only where the database's structure may change.
 */
export function PropertyAppEditorChoice({ property, writable, onUpdate }: {
  property: GraphProperty;
  writable: boolean;
  onUpdate(property: GraphProperty): void;
}) {
  const { t, i18n } = useTranslation("tableView");
  const { editors, current } = usePropertyAppEditors(property);
  const [choosing, setChoosing] = useState(false);
  const chosen = editors.find(({ app, editor }) => app.appId === current?.app && editor.key === current?.editor);
  if (editors.length === 0 && !current) return null;
  const choose = (next?: { app: string; editor: string }) => {
    const config = { ...property.config };
    if (next) config.appEditor = next;
    else delete config.appEditor;
    onUpdate({ ...property, config });
    setChoosing(false);
  };
  const leafen = t("appEditor.leafen");
  return (
    <div className={styles.layoutSettings} data-property-app-editor>
      <button type="button" className={`${styles.layoutRow} ${styles.layoutChoiceRow}`} aria-expanded={choosing}
        disabled={!writable} onClick={() => setChoosing((value) => !value)} data-property-app-editor-row>
        <span className={styles.layoutRowLabel}><ServerAppsIcon size="regular" aria-hidden="true" />{t("appEditor.label")}</span>
        <span className={styles.layoutChoiceValue}>
          <span data-sheet-value>{chosen ? appText(chosen.editor.name, i18n.language) : current ? t("appEditor.unavailable") : leafen}</span>
          <ChevronRight size="regular" aria-hidden="true" />
        </span>
      </button>
      {choosing ? (
        <div className={styles.layoutSettings} role="radiogroup" aria-label={t("appEditor.label")}>
          {[{ key: "", label: leafen, hint: "", next: undefined as { app: string; editor: string } | undefined },
            ...editors.map(({ app, editor }) => ({
              key: `${app.appId}:${editor.key}`, label: appText(editor.name, i18n.language),
              hint: t("appEditor.mark", { app: appText(app.name, i18n.language) }), next: { app: app.appId, editor: editor.key },
            }))].map((choice) => {
            const selected = choice.next ? choice.next.app === current?.app && choice.next.editor === current?.editor : !current;
            return (
              <button key={choice.key} type="button" role="radio" aria-checked={selected} className={`${styles.layoutRow} ${styles.layoutChoiceRow}`}
                data-property-app-editor-choice={choice.key} onClick={() => choose(choice.next)}>
                <span className={styles.layoutRowLabel}>{choice.label}</span>
                <span className={styles.layoutChoiceValue}>{choice.hint}{selected ? <CheckIcon size="small" aria-hidden="true" /> : null}</span>
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

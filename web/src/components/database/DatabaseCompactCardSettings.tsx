import { SwitchInput } from "@/components/Switch";
import type { GraphProperty, GraphView } from "@/contentGraph";
import { EyeIcon } from "@/icons/leafen";
import { compactFullLineIds } from "./cardFullLine";
import { databaseViewLabels } from "./databaseViewLabels";
import { PropertyIcon } from "./PropertyIcon";
import { useOpenSettingsScreen } from "./settingsScreenHead";
import styles from "./DatabaseCompactCardSettings.module.css";

/**
 * "컴팩트 카드 설정", as Notion's (seen 2026-11-03, a gallery's layout with
 * "작게" chosen): a picture of a compact card, what turning a property on
 * does, then "전체 줄 표시" with a switch for each property the cards show —
 * the title's on and fixed, the others off at first — and "속성 표시하기",
 * which opens the properties shown.
 */
export function DatabaseCompactCardSettings({ view, properties, disabled, onUpdateView }: {
  view: GraphView;
  properties: readonly GraphProperty[];
  disabled?: boolean;
  onUpdateView(view: GraphView): void;
}) {
  const labels = databaseViewLabels();
  const open = useOpenSettingsScreen();
  const full = compactFullLineIds(view.config);
  const shown = cardShownProperties(view, properties);
  const turn = (id: string, on: boolean) => {
    const next = [...full].filter((each) => each !== id).concat(on ? [id] : []);
    onUpdateView({ ...view, config: { ...view.config, compactFullLineProperties: next.length ? next : undefined } });
  };
  return (
    <div className={styles.screen} data-compact-card-settings>
      <div className={styles.picture} aria-hidden="true">
        <span className={styles.pictureSwitch} />
        <span className={styles.pictureCard}>
          <span className={styles.pictureLine}><b /><i /><i data-long /></span>
          <span className={styles.pictureLine}><i /><i /><i data-long /></span>
        </span>
      </div>
      <p className={styles.hint}>{labels.compactCardHint}</p>
      <div className={styles.label}>{labels.compactFullLine}</div>
      {shown.map((property) => {
        const title = property.type === "title";
        return (
          <label key={property.id} className={styles.row} data-compact-full-line={property.id}>
            <PropertyIcon property={property} size="small" />
            <span className={styles.name}>{property.name}</span>
            <SwitchInput checked={title || full.has(property.id)} disabled={disabled || title}
              onChange={(event) => turn(property.id, event.currentTarget.checked)} />
          </label>
        );
      })}
      {open && (
        <button type="button" className={styles.showProperties} disabled={disabled} onClick={() => open("properties")}>
          <EyeIcon size="small" aria-hidden="true" />
          <span>{labels.compactShowProperties}</span>
        </button>
      )}
    </div>
  );
}

/** The properties the view's cards show, title first, in the view's order (visibleViewProperties). */
function cardShownProperties(view: GraphView, properties: readonly GraphProperty[]) {
  const config = view.config as Record<string, unknown>;
  const visible = Array.isArray(config.visibleProperties) ? config.visibleProperties as string[] : null;
  const hidden = new Set(Array.isArray(config.hiddenProperties) ? config.hiddenProperties as string[] : []);
  const order = Array.isArray(config.propertyOrder) ? config.propertyOrder as string[] : [];
  const rank = (property: GraphProperty) => property.type === "title" ? -1 : order.includes(property.id) ? order.indexOf(property.id) : order.length;
  return properties.filter((property) => property.type === "title" || (visible ? visible.includes(property.id) : !hidden.has(property.id)))
    .map((property, index) => ({ property, index })).sort((a, b) => rank(a.property) - rank(b.property) || a.index - b.index)
    .map(({ property }) => property);
}

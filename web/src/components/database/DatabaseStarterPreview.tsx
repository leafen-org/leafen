import type { GraphProperty, GraphRow, GraphView } from "@/contentGraph";
import { databaseStarterLabel as label, type DatabaseStarterAuthor } from "@/lib/databaseStarterCatalog";
import { formatDateForProperty, formatTimestampForProperty } from "./dateUtils";
import { databaseStarterPreviewModel } from "./databaseStarterPreviewModel";
import { tableColumnSettings } from "./tablePropertyMenuModel";
import { projectVisibleProperties } from "./tableViewModel";
import { compareTableGroups, tableGrouper } from "./tableGrouping";
import styles from "./DatabaseStarterGallery.module.css";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { displayDate } from "@/lib/displayDate";

type Option = { id: string; name: string; color?: string };
function options(property: GraphProperty): Option[] {
  return Array.isArray(property.config.options) ? property.config.options as Option[] : [];
}
function value(property: GraphProperty, format: (text: string) => string, row?: GraphRow, sample = 0, model?: ReturnType<typeof databaseStarterPreviewModel>, author?: DatabaseStarterAuthor) {
  const raw = row?.values[property.id]?.value;
  if (property.type === "select" || property.type === "status") {
    const option = row ? options(property).find(o => o.id === raw) : options(property)[sample % (options(property).length || 1)];
    return option ? <span className={styles.tag} data-color={option.color}>{format(option.name)}</span> : null;
  }
  if (row && property.type === "multi_select") {
    const ids = Array.isArray(raw) ? raw : [];
    return <>{options(property).filter(o => ids.includes(o.id)).map(o => <span key={o.id} className={styles.tag} data-color={o.color}>{format(o.name)}</span>)}</>;
  }
  if (row && property.type === "formula") return model?.formula(property, row);
  if (row && property.type === "button") return <span className={styles.tag}>{format(String((property.config.button as {label?: string})?.label ?? property.name))}</span>;
  if (property.type.endsWith("_by")) return <span><span className={styles.avatar} />{row ? author?.name : null}</span>;
  if (row && (property.type === "created_time" || property.type === "last_edited_time")) return formatTimestampForProperty(property.type === "created_time" ? row.createdAt : row.updatedAt, property);
  if (!row) return property.type === "person" || property.type.endsWith("_by") ? <span className={styles.avatar} /> : <span className={styles.skeleton} />;
  if (typeof raw === "string" || typeof raw === "number") return format(String(raw));
  if (property.type === "date") return formatDateForProperty(raw, property);
  return null;
}

/** Metadata thumbnails never fetch rows. The customizer uses the exported examples. */
export function DatabaseStarterPreview({ title, icon, properties, rows, view, thumbnail = false, heading = true, localize = true, author }: {
  title: string; icon?: string; properties: readonly GraphProperty[]; rows?: readonly GraphRow[]; view?: GraphView; thumbnail?: boolean; heading?: boolean; localize?: boolean;
  author?: DatabaseStarterAuthor;
}) {
  const format = localize ? label : (text: string) => text;
  const shown = thumbnail ? properties.slice(0, 3) : projectVisibleProperties({properties, ...tableColumnSettings(view?.config ?? {})});
  const model = databaseStarterPreviewModel(properties, rows ?? [], view, author?.id);
  const examples: (GraphRow | undefined)[] = rows ? model.rows : Array.from({ length: 3 }, () => undefined);
  const display = (p: GraphProperty, row?: GraphRow, i = 0) => value(p, format, row, i, model, author);
  const name = properties.find(p => p.type === "title");
  const titleNode = heading && !thumbnail ? <strong><StarterTemplateIcon icon={icon} size={20} /> {title}</strong> : null;
  if (!thumbnail && view?.type === "list") {
    const status = properties.find(p => p.id === view.config.statusCheckboxPropertyId);
    return <div className={styles.preview}>{titleNode}{examples.map((row, i) => <div className={styles.listRow} key={row?.id ?? i}>
      {status ? <span className={styles.completion} aria-hidden="true">{row?.values[status.id]?.value === options(status).at(-1)?.id ? "✓" : row?.values[status.id]?.value === options(status)[1]?.id ? "−" : ""}</span> : null}
      {name ? display(name, row, i) : null}
    </div>)}</div>;
  }
  if (!thumbnail && view?.type === "timeline") {
    const start = properties.find(p => p.id === view.config.timelineBy), end = properties.find(p => p.id === view.config.timelineEndBy);
    const date = (p: GraphProperty | undefined, row?: GraphRow) => { const v = p && row?.values[p.id]?.value; return v && typeof v === "object" && "start" in v ? Date.parse(String(v.start)) : NaN; };
    const dates = examples.flatMap(row => [date(start, row), date(end, row)]).filter(Number.isFinite);
    const first = dates.length ? Math.min(...dates) : 0, last = dates.length ? Math.max(...dates) : 1, span = Math.max(last-first, 86400000);
    const group = properties.find(p => p.id === view.config.groupBy);
    const groups = group ? options(group).map(option => ({option, rows: examples.filter(row => row?.values[group.id]?.value === option.id)})) : [{option: undefined, rows: examples}];
    return <div className={styles.preview}>{titleNode}<div className={styles.timelineHeader}>{displayDate(dates.length ? first : Date.now())}<span>{dates.length ? displayDate(last) : ""}</span></div>
      {groups.map(({option, rows: groupedRows}) => <div key={option?.id ?? "all"}>{option ? <span className={styles.tag} data-color={option.color}>{format(option.name)}</span> : null}
      {groupedRows.map((row, i) => <div className={styles.timelineRow} key={row?.id ?? i}><span>{name ? display(name, row, i) : null}</span><div>
        {Number.isFinite(date(start,row)) ? <i style={{left: ((date(start,row)-first)/span*80)+"%",width:Math.max(5,Number.isFinite(date(end,row)) ? (date(end,row)-date(start,row))/span*80 : 5)+"%"}} /> : null}
      </div></div>)}</div>)}</div>;
  }
  if (view?.type === "board") {
    const group = properties.find(p => p.id === view.config.groupBy);
    return <div className={styles.preview} data-board data-thumbnail={thumbnail || undefined}>
      {titleNode}
      {/* A status group's head is its pill with a dot, and a card's title follows its page's icon, as Notion's (seen 2026-11-11). */}
      <div className={styles.board}>{(group ? options(group) : [{ id: "", name: "" }]).map(option => <div key={option.id} data-color={option.color}>
        <span className={styles.tag} data-color={option.color} data-status={group?.type === "status" || undefined}>
          {group?.type === "status" ? <i className={styles.statusDot} aria-hidden="true" /> : null}{format(option.name)}</span>
        {(rows ? examples.filter(row => !group || row?.values[group.id]?.value === option.id) : [undefined]).map((row, i) => <div className={styles.boardCard} key={row?.id ?? i}>
          {shown.filter(p => p.type === "title").map(p => <strong key={p.id}>{!thumbnail && row && row.iconType && row.iconType !== "none"
            ? <span className={styles.cardIcon}><PageIconGlyph page={{ icon: row.icon ?? "", iconType: row.iconType }} size={16} fallback="none" /></span> : null}{display(p, row, i)}</strong>)}
          {!thumbnail ? shown.filter(p => p.type === "select").slice(0, 1).map(p => <span key={p.id}>{display(p, row, i)}</span>) : null}
        </div>)}
      </div>)}</div>
    </div>;
  }
  const groupProperty = !thumbnail && view?.type === "table" ? properties.find(p => p.id === view.config.groupBy) : undefined;
  const grouper = groupProperty ? tableGrouper({ ...groupProperty, name: format(groupProperty.name) }, { config: view?.config ?? {} }) : undefined;
  const groups = new Map<string, { descriptor: ReturnType<NonNullable<typeof grouper>>; rows: typeof examples }>();
  if (grouper && groupProperty) for (const row of examples) {
    const descriptor = grouper(row?.values[groupProperty.id]?.value);
    const group = groups.get(descriptor.key) ?? { descriptor, rows: [] };
    group.rows.push(row); groups.set(descriptor.key, group);
  }
  const table = (entries: typeof examples) => <table><thead><tr>{shown.map(p => <th key={p.id}>{format(p.name)}</th>)}</tr></thead>
    <tbody>{entries.map((row, i) => <tr key={row?.id ?? i}>{shown.map(p => <td key={p.id}>{display(p, row, i)}</td>)}</tr>)}</tbody></table>;
  return <div className={styles.preview} data-thumbnail={thumbnail || undefined}>
    {titleNode}
    <div className={styles.previewScroll}>
      {grouper ? [...groups.values()].sort((a, b) => compareTableGroups(a.descriptor, b.descriptor)).map(group => <div key={group.descriptor.key} data-starter-preview-group={group.descriptor.key}>
        <span className={styles.tag} data-color={group.descriptor.color}>{format(group.descriptor.label)}</span>{table(group.rows)}
      </div>) : table(examples)}
    </div>
  </div>;
}

/** A template's icon: a built-in one ("checkmark:green", the task tracker's as Notion's, seen 2026-11-05) drawn, an emoji written. */
export function StarterTemplateIcon({ icon, size }: { icon?: string; size: number }) {
  if (!icon) return null;
  return /^[a-z0-9-]+:[a-z]+$/.test(icon) ? <PageIconGlyph page={{ icon, iconType: "icon" }} size={size} /> : <>{icon}</>;
}

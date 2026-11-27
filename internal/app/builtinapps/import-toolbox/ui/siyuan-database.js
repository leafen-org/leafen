// SiYuan's databases — attribute views, each a JSON file at
// data/storage/av/<id>.json (kernel/av in SiYuan's source) — as Leafen
// databases where their blocks stand: the view's name, its columns as
// properties (the primary key the title; text, number, date, select,
// multi-select, URL, email, phone and checkbox as they are; created and
// updated as dates; a relation's or an asset's names as text), its items as
// rows in the first view's order, and its table, list, gallery, kanban and
// calendar views. An item bound to a document or a block says so in its
// page with a mention of that document. Another block of the same view
// mentions the database.

// An option's color is SiYuan's --b3-font-background<n> (its light theme):
// the card colors (error, warning, info, success), then gray, blue, orange,
// red, pink, cyan, green, yellow and the theme's own.
const COLORS = { 1: "red", 2: "orange", 3: "blue", 4: "green", 5: "gray", 6: "blue", 7: "orange", 8: "red", 9: "pink", 10: "blue",
  11: "green", 12: "yellow", 13: "gray", 14: "brown" };
const TYPES = {
  block: "title", text: "rich_text", number: "number", date: "date", select: "select", mSelect: "multi_select", url: "url",
  email: "email", phone: "phone", checkbox: "checkbox", created: "date", updated: "date", relation: "rich_text", mAsset: "rich_text",
};
const VIEWS = { table: "table", list: "list", gallery: "gallery", kanban: "board", calendar: "calendar" };

function stableId(text) {
  let hash = 0x811c9dc5;
  for (const char of text) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 0x01000193) >>> 0;
  }
  return hash.toString(16).padStart(8, "0");
}
const key15 = (text) => `${stableId(text)}${stableId(`${text}\u0000`)}`.slice(0, 15);

/** The attribute view a ZIP of SiYuan's data holds for an id, or null. */
export async function readAttributeView(zip, id) {
  const path = zip.names.find((name) => name === `storage/av/${id}.json` || name.endsWith(`/storage/av/${id}.json`));
  if (!path) return null;
  try {
    return JSON.parse(await zip.text(path));
  } catch {
    return null;
  }
}

/** A SiYuan date (milliseconds) as Leafen's: a day in the reader's time zone, or a moment. */
function dateOf(milliseconds, dayOnly) {
  if (!milliseconds) return "";
  const at = new Date(milliseconds);
  if (!dayOnly) return at.toISOString();
  const pad = (number) => String(number).padStart(2, "0");
  return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
}

/**
 * Writes the database `av` stands for where its block `node` stands, under
 * `parentId` on `pageId`; `docOf(blockId)` names the document a SiYuan block
 * is in. A view written before is mentioned instead (`written` keeps them).
 */
export function writeAttributeView({ bundle, pageId, parentId, node, av, written, docOf, labels, lose }) {
  const blockId = `${pageId}-${node.ID}`;
  if (written.has(av.id)) {
    bundle.block({ id: blockId, pageId, parentId, type: "paragraph", content: {},
      text: [{ insert: av.name || labels.untitled, attributes: { mention: "database", databaseId: written.get(av.id) } }] });
    return;
  }
  written.set(av.id, blockId);
  const dataSourceId = `av-${av.id}`;
  const propertyId = (keyId) => `av-${av.id}-${keyId}`;
  const keyValues = (av.keyValues ?? []).filter((entry) => entry?.key?.id);
  const order = av.keyIDs?.length ? av.keyIDs : keyValues.map((entry) => entry.key.id);
  const ordered = [...keyValues].sort((left, right) => order.indexOf(left.key.id) - order.indexOf(right.key.id));
  const kept = [];
  let computed = 0;
  let asText = 0;
  for (const { key } of ordered) {
    const type = TYPES[key.type];
    if (!type) {
      computed += 1;
      continue;
    }
    if (key.type === "relation" || key.type === "mAsset") asText += 1;
    const config = type === "select" || type === "multi_select" ? {
      options: (key.options ?? []).map((option) => ({
        id: `o${key15(`${av.id}\u0000${key.id}\u0000${option.name}`)}`, name: option.name,
        color: COLORS[option.color] ?? "default",
      })),
    } : {};
    kept.push({ key, property: { id: propertyId(key.id), name: key.name || "", type, config } });
  }
  if (!kept.some(({ property }) => property.type === "title")) {
    kept.unshift({ key: { id: "", type: "block" }, property: { id: propertyId("title"), name: "", type: "title", config: {} } });
  }
  if (computed) lose(labels.lossAvComputed, labels.lossAvComputedWhy, computed);
  if (asText) lose(labels.lossAvAsText, labels.lossAvAsTextWhy, asText);

  const views = [];
  for (const view of av.views ?? []) {
    const type = VIEWS[view.type] ?? "table";
    const config = {};
    const field = view.group?.field;
    const grouped = kept.find(({ key }) => key.id === field && ["select", "mSelect"].includes(key.type));
    if (type === "board" && grouped) config.groupBy = grouped.property.id;
    if (type === "calendar") {
      const dated = kept.find(({ property }) => property.type === "date");
      if (!dated) {
        views.push({ id: `av-${av.id}-${view.id}`, name: view.name ?? "", type: "table", config: {} });
        continue;
      }
      config.calendarBy = dated.property.id;
    }
    views.push({ id: `av-${av.id}-${view.id}`, name: view.name ?? "", type, config });
  }
  if (views.length === 0) views.push({ id: `av-${av.id}-table`, name: "", type: "table", config: {} });
  bundle.inlineDatabase({ id: blockId, pageId, parentId, title: av.name ?? "", dataSourceId, properties: kept.map(({ property }) => property), views });

  // The items, in the first view's order, then any it doesn't list.
  const titles = keyValues.find((entry) => entry.key.type === "block")?.values ?? [];
  const items = new Map(titles.map((value) => [value.blockID, value]));
  for (const { values = [] } of keyValues) for (const value of values) if (value?.blockID && !items.has(value.blockID)) items.set(value.blockID, null);
  const first = av.views?.[0];
  const listed = first?.itemIds ?? first?.table?.rowIds ?? [];
  const itemIds = [...new Set([...listed.filter((id) => items.has(id)), ...items.keys()])];
  for (const itemId of itemIds) {
    const titleValue = items.get(itemId);
    const values = {};
    for (const { key, property } of kept) {
      if (!key.id) continue;
      const value = keyValues.find((entry) => entry.key.id === key.id)?.values?.find((candidate) => candidate?.blockID === itemId);
      const cell = cellOf(key, property, value, titleValue, av);
      if (cell !== undefined) values[property.id] = { type: property.type, value: cell };
    }
    const title = kept.find(({ property }) => property.type === "title").property.id;
    values[title] ??= { type: "title", value: titleValue?.block?.content ?? "" };
    const rowId = `av-${av.id}-${itemId}`;
    bundle.row({ id: rowId, databaseId: pageId, dataSourceId, values });
    // An item bound to a document or a block names that document in its page.
    const bound = titleValue && !titleValue.isDetached ? titleValue.block?.id || itemId : "";
    const document = bound ? docOf(bound) : "";
    if (document) {
      const at = titleValue.block?.created || titleValue.createdAt || 1;
      bundle.rowBlock({ id: `${rowId}-doc`, rowId, databaseId: pageId, type: "paragraph", content: {},
        text: [{ insert: titleValue.block?.content || labels.untitled, attributes: { mention: "page", pageId: `doc-${document}` } }],
        createdAt: at, updatedAt: titleValue.block?.updated || at });
    }
  }
}

/** One item's value in a column, as Leafen's database API takes it, or undefined for none. */
function cellOf(key, property, value, titleValue, av) {
  switch (key.type) {
    case "block": return value?.block?.content ?? titleValue?.block?.content ?? "";
    case "text": return value?.text?.content || undefined;
    case "number": return value?.number?.isNotEmpty ? value.number.content : undefined;
    case "date": {
      const date = value?.date;
      if (!date?.isNotEmpty) return undefined;
      const start = dateOf(date.content, date.isNotTime);
      const end = date.hasEndDate && date.isNotEmpty2 ? dateOf(date.content2, date.isNotTime) : "";
      return end ? { start, end } : { start };
    }
    case "created": case "updated": {
      const stamp = value?.[key.type]?.content || (key.type === "created" ? titleValue?.block?.created : titleValue?.block?.updated);
      return stamp ? { start: dateOf(stamp, false) } : undefined;
    }
    case "select": case "mSelect": {
      const names = (value?.mSelect ?? []).map((choice) => choice.content);
      const ids = property.config.options.filter((option) => names.includes(option.name)).map((option) => option.id);
      if (ids.length === 0) return undefined;
      return property.type === "select" ? ids[0] : ids;
    }
    case "url": return value?.url?.content || undefined;
    case "email": return value?.email?.content || undefined;
    case "phone": return value?.phone?.content || undefined;
    case "checkbox": return value?.checkbox ? Boolean(value.checkbox.checked) : undefined;
    case "relation": {
      const names = (value?.relation?.contents ?? []).map((item) => item?.block?.content).filter(Boolean);
      if (names.length === 0 && av) {
        const own = (av.keyValues ?? []).find((entry) => entry.key.type === "block")?.values ?? [];
        for (const id of value?.relation?.blockIDs ?? []) {
          const found = own.find((candidate) => candidate.blockID === id)?.block?.content;
          if (found) names.push(found);
        }
      }
      return names.length ? names.join(", ") : undefined;
    }
    case "mAsset": {
      const names = (value?.mAsset ?? []).map((asset) => asset.name || asset.content).filter(Boolean);
      return names.length ? names.join(", ") : undefined;
    }
    default: return undefined;
  }
}

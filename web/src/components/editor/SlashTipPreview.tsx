import type { CSSProperties, ReactNode } from "react";
import { blockDefKey, type BlockDef } from "./blockDefinitions";
import styles from "./SlashTipPreview.module.css";

// Notion's slash tip shows a 140×100 picture of the block above its
// description (seen 2026-10-01) for the basic blocks, media but audio, the
// database views and charts, the form, the advanced blocks and the inline
// mentions; embeds, imports and the line's actions have none. Leafen draws
// its own small sample of each block rather than Notion's pictures.

const bar = (width: number, variant = "", style?: CSSProperties) => (
  <span className={`${styles.bar} ${variant}`} style={{ width: `${width}%`, ...style }} />
);

const row = (marker: ReactNode, width: number, variant = "") => (
  <span className={styles.row}>
    <span className={styles.marker}>{marker}</span>
    {bar(width, variant)}
  </span>
);

const text = (...widths: number[]) => widths.map((width, index) => <span key={index} className={styles.row}>{bar(width)}</span>);

const HEADING_SIZES: Record<string, string> = {
  "1": styles.h1, "2": styles.h2, "3": styles.h3, "4": styles.h4,
};

function heading(level: string, toggle: boolean) {
  return (
    <>
      <span className={styles.row}>
        {toggle ? <span className={styles.marker}>▸</span> : null}
        {bar(70, HEADING_SIZES[level])}
      </span>
      {text(90, 65)}
    </>
  );
}

const grid = (columns: number, rows: number, className = "") => (
  <span className={`${styles.grid} ${className}`} style={{ gridTemplateColumns: `repeat(${columns}, 1fr)` }}>
    {Array.from({ length: columns * rows }, (_, index) => <span key={index} className={styles.cell} />)}
  </span>
);

const card = (children: ReactNode, className = "") => <span className={`${styles.card} ${className}`}>{children}</span>;

function chart(key: string) {
  if (key === "chart_bar") {
    return <span className={styles.bars}>{[60, 85, 40, 70, 95].map((height, index) => <span key={index} style={{ height: `${height}%` }} />)}</span>;
  }
  if (key === "chart_horizontal_bar") {
    return <span className={styles.rows}>{[85, 55, 70, 35].map((width, index) => <span key={index} className={styles.row}>{bar(width, styles.accent)}</span>)}</span>;
  }
  if (key === "chart_line") {
    return (
      <svg className={styles.svg} viewBox="0 0 120 70" role="presentation">
        <polyline points="4,60 28,42 52,48 76,22 100,30 116,10" fill="none" stroke="currentColor" strokeWidth="3" strokeLinejoin="round" />
      </svg>
    );
  }
  if (key === "chart_donut") {
    return (
      <svg className={styles.svg} viewBox="0 0 70 70" role="presentation">
        <circle cx="35" cy="35" r="24" fill="none" style={{ stroke: "color-mix(in srgb, var(--text-default) 12%, transparent)" }} strokeWidth="12" />
        <circle cx="35" cy="35" r="24" fill="none" stroke="currentColor" strokeWidth="12" strokeDasharray="100 151" transform="rotate(-90 35 35)" />
      </svg>
    );
  }
  return <span className={styles.number}>42</span>;
}

function view(key: string) {
  switch (key) {
    case "inline_database_board":
      return (
        <span className={styles.columns}>
          {[2, 3, 1].map((cards, index) => (
            <span key={index}>{Array.from({ length: cards }, (_, at) => <span key={at}>{card(bar(70), styles.small)}</span>)}</span>
          ))}
        </span>
      );
    case "inline_database_gallery":
      return <span className={styles.gallery}>{Array.from({ length: 4 }, (_, index) => <span key={index}>{card(bar(60))}</span>)}</span>;
    case "inline_database_list":
      return <span className={styles.rows}>{[70, 55, 80].map((width, index) => <span key={index} className={styles.row}><span className={styles.marker}>▫</span>{bar(width)}<span className={styles.pill} /></span>)}</span>;
    case "inline_database_feed":
      return <span className={styles.rows}>{[0, 1].map((index) => <span key={index}>{card(<><span className={styles.row}><span className={styles.dot} />{bar(50, styles.strong)}</span>{text(90)}</>)}</span>)}</span>;
    case "inline_database_dashboard":
      return <span className={styles.gallery}><span>{card(<span className={styles.number}>7</span>)}</span><span>{card(chart("chart_bar"))}</span><span>{card(chart("chart_line"))}</span><span>{card(text(80, 60))}</span></span>;
    case "inline_database_calendar":
      return <>{grid(7, 4, styles.calendar)}</>;
    case "inline_database_timeline":
      return <span className={styles.rows}>{[[0, 45], [25, 50], [50, 40], [15, 30]].map(([left, width], index) => <span key={index} className={styles.row}>{bar(width, styles.accent, { marginInlineStart: `${left}%` })}</span>)}</span>;
    case "inline_database_map":
      return <span className={styles.map}><span className={styles.pin} /></span>;
    case "inline_database_form":
      return <span className={styles.rows}>{bar(50, styles.strong)}<span className={styles.input} />{bar(40)}<span className={styles.input} /><span className={styles.button} /></span>;
    default:
      return <>{bar(45, styles.strong)}{grid(3, 4, styles.table)}</>;
  }
}

function preview(key: string, def: BlockDef): ReactNode {
  if (key.startsWith("heading_")) return heading(key.slice(-1), false);
  if (key.startsWith("toggle_heading_")) return heading(key.slice(-1), true);
  if (key.startsWith("chart_")) return chart(key);
  if (key.startsWith("inline_database") || key === "child_database") return view(key);
  if (key.startsWith("column_list_")) {
    return <span className={styles.columns}>{Array.from({ length: def.columnCount ?? 2 }, (_, index) => <span key={index}>{text(90, 70, 80)}</span>)}</span>;
  }
  switch (key) {
    case "paragraph": return text(95, 80, 90, 55);
    case "bulleted_list_item": return <>{row("•", 70)}{row("•", 55)}{row("•", 80)}</>;
    case "numbered_list_item": return <>{row("1.", 70)}{row("2.", 55)}{row("3.", 80)}</>;
    case "to_do": return <>{row(<span className={`${styles.check} ${styles.checked}`} />, 60, styles.done)}{row(<span className={styles.check} />, 75)}{row(<span className={styles.check} />, 50)}</>;
    case "toggle": return <>{row("▸", 70)}<span className={styles.indent}>{text(80, 60)}</span></>;
    case "child_page": return <>{row(<span className={styles.page} />, 55, styles.underline)}</>;
    case "link_to_page": return <>{row("↗", 55, styles.underline)}</>;
    case "callout": return card(<>{row("💡", 75)}{text(85)}</>, styles.callout);
    case "quote": return <span className={styles.quote}>{text(90, 75, 60)}</span>;
    case "simple_table": return grid(3, 3, styles.table);
    case "divider": return <>{text(85)}<span className={styles.rule} />{text(70)}</>;
    case "image": return <span className={styles.picture}><span className={styles.sun} /><span className={styles.hill} /></span>;
    case "video": return <span className={styles.video}><span className={styles.play} /></span>;
    case "code": case "code_mermaid": return card(<>{bar(55, styles.code)}{bar(75, styles.code)}{bar(40, styles.code)}</>, styles.codeBox);
    case "file": return <>{row(<span className={styles.page} />, 50)}{bar(25)}</>;
    case "bookmark": return card(<span className={styles.bookmark}><span>{bar(80, styles.strong)}{text(90, 60)}</span><span className={styles.thumb} /></span>);
    case "table_of_contents": return <span className={styles.rows}>{bar(60, styles.link)}<span className={styles.indent}>{bar(70, styles.link)}{bar(50, styles.link)}</span>{bar(55, styles.link)}</span>;
    case "equation": return <span className={styles.math}>E = mc²</span>;
    case "button": return <span className={styles.rows}><span className={styles.button} />{text(70)}</span>;
    case "breadcrumb": return <span className={styles.row}>{bar(25)}<span className={styles.marker}>/</span>{bar(25)}<span className={styles.marker}>/</span>{bar(25)}</span>;
    case "tab": return <><span className={styles.tabs}><span className={styles.activeTab} /><span /><span /></span>{text(90, 65)}</>;
    case "synced_block": return <span className={styles.synced}>{text(85, 70)}</span>;
    case "inline_person": return <span className={styles.row}>{bar(30)}<span className={styles.mention}>@</span>{bar(30)}</span>;
    case "inline_page": return <span className={styles.row}>{bar(25)}<span className={styles.marker}>↗</span>{bar(35, styles.underline)}</span>;
    case "inline_date": return <span className={styles.row}>{bar(25)}<span className={styles.mention}>@</span>{bar(30, styles.accent)}</span>;
    case "inline_emoji": return <span className={styles.row}>{bar(40)}<span className={styles.emoji}>🙂</span>{bar(25)}</span>;
    case "inline_equation": return <span className={styles.row}>{bar(30)}<span className={styles.inlineMath}>x²</span>{bar(30)}</span>;
    default: return null;
  }
}

/** The sample of a slash item's block above its description, or none. */
export function SlashTipPreview({ def }: { def: BlockDef }) {
  if (def.action || def.group === "Embed" || def.group === "Apps" || def.group === "Import" || def.group === "Actions") return null;
  const key = blockDefKey(def);
  if (key === "audio" || key === "pdf" || key.startsWith("embed")) return null;
  const sample = preview(key, def);
  if (!sample) return null;
  return <span className={styles.preview} aria-hidden="true" data-slash-tip-preview={key}>{sample}</span>;
}

import { useId } from "react";
import type { PropertyAccessLevel } from "@/lib/propertyAccess";
import css from "./PropertyAccessLevelPicture.module.css";

/** What a level's picture marks: the column a person edits or sees, its values alone, its head alone, or none of it. */
type Mark = "inherit" | "edit" | "editValues" | "view" | "viewValues" | "viewSchema" | "none" | "noneValues";

function markOf(level: PropertyAccessLevel, valuesOnly: boolean): Mark {
  switch (level) {
    case "edit": return "edit";
    case "edit_values": return "editValues";
    case "view": return valuesOnly ? "viewValues" : "view";
    case "view_property": return "viewSchema";
    case "none": return valuesOnly ? "noneValues" : "none";
    default: return "inherit";
  }
}

/** A box's edges as one path, each a `width` line centred on them. */
function box(x: number, y: number, width: number, height: number) {
  return `M${x} ${y}H${x + width}V${y + height}H${x}Z`;
}

/** Lines 45° down across a column from `x` to `right`, 2.5px apart square to them; `start` is the first one's y − x, where Notion's begin. */
function hatch(x: number, top: number, right: number, start: number) {
  const lines: string[] = [];
  for (let offset = start; offset > top - right - 2; offset -= 2.5 * Math.SQRT2) {
    lines.push(`M${x - 4} ${x - 4 + offset}L${right + 4} ${right + 4 + offset}`);
  }
  return lines.join("");
}

/**
 * A level's picture, as Notion's (its menus, preview and level box; read
 * 2026-11-18 in Chrome at 1440 in a Business database, every level, light and
 * dark): a small table whose third column is the property. The menu's and the
 * preview's is 37×25 — four columns of 9 under a head row filled in the faint
 * tint, 4 round, in the strong border's lines; the level box's (`mini`) is
 * 17×14 — a 2 round frame in the third grey with a head line, its middle
 * column the property. The property's column is filled where the level
 * reaches: the whole column to edit (purple) or see (blue), the body alone for
 * values only, the head alone for the property only (its body left plain),
 * struck through red for none; "데이터베이스에서 상속" the bare table (the
 * mini a 2×3 table). A person property's own levels reach its values only.
 */
export function LevelPicture({ level, size = "menu", valuesOnly = false }: {
  level: PropertyAccessLevel;
  size?: "menu" | "mini";
  valuesOnly?: boolean;
}) {
  const clip = useId();
  const mark = markOf(level, valuesOnly);
  const tone = mark === "edit" || mark === "editValues" ? "edit" : mark.startsWith("view") ? "view" : "none";
  const values = `var(--level-${tone}-values)`, line = `var(--level-${tone})`;
  if (size === "mini") {
    // Its middle column, 5 wide from 5.625; the head 4 tall, the body 8 under it; lines 1.25.
    const head = box(5.625, 0.625, 5, 4), body = box(5.625, 4.625, 5, 8), column = box(5.625, 0.625, 5, 12);
    const frame = "M2.625 0.625H13.625A2 2 0 0 1 15.625 2.625V10.625A2 2 0 0 1 13.625 12.625H2.625A2 2 0 0 1 0.625 10.625V2.625A2 2 0 0 1 2.625 0.625Z";
    const rows = mark === "inherit" ? "M0.625 4.625H15.625M0.625 8.625H15.625M8.125 0.625V12.625" : mark === "none" ? "" : "M0.625 4.625H15.625";
    const split = mark === "editValues" || mark === "viewValues" || mark === "viewSchema" || mark === "noneValues";
    const struck = mark === "none" || mark === "noneValues" ? (mark === "none" ? column : body) : null;
    return (
      <svg className={css.picture} viewBox="0 0 17 14" width={17} height={14} fill="none" aria-hidden="true" data-level-glyph={level}>
        {split ? <path d="M5.625 0.625V12.625M10.625 0.625V12.625" stroke="var(--level-grid-line)" strokeWidth={1.25} /> : null}
        <path d={frame + rows} stroke="var(--level-frame)" strokeWidth={1.25} />
        {mark === "edit" || mark === "view" ? <path d={`${body}${head}`} fill={values} stroke={line} strokeWidth={1.25} /> : null}
        {mark === "editValues" || mark === "viewValues" ? <path d={body} fill={values} stroke={line} strokeWidth={1.25} /> : null}
        {mark === "viewSchema" ? <path d={head} fill={values} stroke={line} strokeWidth={1.25} /> : null}
        {struck ? <>
          <clipPath id={clip}><path d={struck} /></clipPath>
          <path d={struck} fill="var(--level-none-fill)" />
          <path d={hatch(5.625, mark === "none" ? 0.625 : 4.625, 10.625, 7.26)} stroke="var(--level-none-hatch)" strokeWidth={1.25}
            clipPath={`url(#${clip})`} />
          <path d={struck} stroke={line} strokeWidth={1.25} />
        </> : null}
      </svg>
    );
  }
  // Columns of 9 at 0.5, 9.5, 18.5, 27.5; rows at 0.5, 8.5, 16.5; the property's column from 18.5.
  const head = box(18.5, 0.5, 9, 8), body = box(18.5, 8.5, 9, 16), column = box(18.5, 0.5, 9, 24);
  const struck = mark === "none" ? column : mark === "noneValues" ? body : null;
  return (
    <svg className={css.picture} viewBox="0 0 37 25" width={37} height={25} fill="none" aria-hidden="true" data-level-icon={level}>
      <path d="M0.5 8.5V4.5A4 4 0 0 1 4.5 0.5H32.5A4 4 0 0 1 36.5 4.5V8.5Z" fill="var(--level-grid-fill)" />
      <path d="M4.5 0.5H32.5A4 4 0 0 1 36.5 4.5V20.5A4 4 0 0 1 32.5 24.5H4.5A4 4 0 0 1 0.5 20.5V4.5A4 4 0 0 1 4.5 0.5ZM9.5 0.5V24.5M18.5 0.5V24.5M27.5 0.5V24.5M0.5 8.5H36.5M0.5 16.5H36.5"
        stroke="var(--level-grid-line)" />
      {mark === "edit" || mark === "editValues" || mark === "view" || mark === "viewValues" ? <path d={body} fill={values} stroke={line} /> : null}
      {mark === "edit" || mark === "view" || mark === "viewSchema" ? <path d={head} fill={`var(--level-${tone}-head)`} stroke={line} /> : null}
      {struck ? <>
        <clipPath id={clip}><path d={struck} /></clipPath>
        <path d={struck} fill="var(--level-none-fill)" />
        <path d={hatch(18.5, mark === "none" ? 0.5 : 8.5, 27.5, 6.4)} stroke="var(--level-none-hatch)" clipPath={`url(#${clip})`} />
        <path d={struck} stroke={line} />
      </> : null}
    </svg>
  );
}

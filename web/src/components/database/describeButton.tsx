import type { ButtonHTMLAttributes } from "react";
import { InfoFillIcon } from "@/icons/leafen";
import styles from "./describeButton.module.css";

/**
 * Notion's ⓘ's own width in a 16px box: its 20px glyph drawn in a box as
 * wide as its circle, 15.26 across (measured 2026-11-18 in Chrome 1416:
 * infoCircleFill on a view of 2.37 0 15.26 20, its button 15.26×20 in all
 * four places).
 */
const GLYPH_WIDTH = (15.26 * 16) / 20;

/**
 * The ⓘ at the end of a name field that opens a line for a description
 * under it, one rule for every such button (a column menu's head and a
 * property's settings, a view's and a content tab's settings, a select
 * option's menu), as Notion's (measured 2026-11-18 in Chrome 1416, dark and
 * light in two tabs):
 * - its name, which shows in the dark tip over it, centred 6px above it
 *   ("속성 설명 추가" in the column menu's head and in the view settings'
 *   property, "보기 설명 추가" in the view settings' and a content tab's head,
 *   "설명 추가" in a select option's menu — each tip's foot 6 over the ⓘ, all
 *   with room under it), under it only where the window has no room over it
 *   (iconTips);
 * - its box, the 20px glyph as wide as its circle, 15.26×20, rounded whole,
 *   drawn in the secondary icon color and, pointed at, the primary over the
 *   hover tint (describeButton.module.css). Where it sits in its field is the
 *   field's: `className` places it there.
 */
export function DescribeButton({ name, open, className, ...button }: {
  name: string;
  open: boolean;
} & Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children" | "type" | "title" | "aria-label" | "aria-expanded">) {
  return (
    <button type="button" {...button} className={className ? `${styles.describe} ${className}` : styles.describe}
      aria-label={name} title={name} aria-expanded={open} data-tip-side="top">
      <InfoFillIcon size="regular" trim glyphWidth={GLYPH_WIDTH} aria-hidden="true" />
    </button>
  );
}

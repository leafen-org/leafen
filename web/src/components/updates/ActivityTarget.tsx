import type { MouseEvent, ReactNode } from "react";
import type { Page } from "@/types";
import { PageIconGlyph } from "../page/PageIcon";
import styles from "./ActivityTarget.module.css";

/**
 * Where a workspace update's sentence goes, a link pressed alone: a plain
 * press opens it in this tab, a modified one leaves it to the browser.
 */
export function followActivityLink(href: string, open: (href: string) => void) {
  return (event: MouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
    event.preventDefault();
    open(href);
  };
}

/**
 * The page or database a workspace update's sentence names, as Notion's
 * (seen 2026-11-15 at 1440 dark): its own icon, if it has one, in an 18px box 2px
 * and a space before its name, which keeps 3px after it; a page with no icon,
 * a database one too, is its name alone — no document or table glyph stands
 * for a missing icon. The whole keeps 1px either side, in the 600 the
 * sentence names pages in. It is the line's link to what it names, the line
 * around it pressing nothing (Notion's, seen 2026-11-16).
 */
export function ActivityTarget({ page, href, onOpen, children }: {
  page?: Page; href?: string; onOpen?: (href: string) => void; children?: ReactNode;
}) {
  const icon = page && page.iconType !== "none" && page.icon ? page : undefined;
  const inner = <>
    {icon ? <><span className={styles.icon} aria-hidden="true"><PageIconGlyph page={icon} size={16} box={18}/></span>{" "}</> : null}
    <span className={styles.name}>{children}</span>
  </>;
  return href && onOpen
    ? <a className={styles.target} href={href} onClick={followActivityLink(href, onOpen)} data-activity-target={icon ? "icon" : "name"}>{inner}</a>
    : <span className={styles.target} data-activity-target={icon ? "icon" : "name"}>{inner}</span>;
}

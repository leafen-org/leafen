"use client";

import { useTranslation } from "react-i18next";
import { SubitemCountIcon, SubitemParentIcon } from "@/icons/leafen";
import { usePageNames } from "@/lib/pageNames";
import type { Page } from "@/lib/types";
import type { CardViewSubitemPresentation } from "./queryProjection";
import styles from "./CardSubitemMeta.module.css";

type CardSubitemPresentation = Pick<CardViewSubitemPresentation, "childCountByParent" | "childrenPropertyId" | "mode" | "scope">;

/** A card's own properties: with sub-items as a card property, theirs only when it has some. */
export function rowCardProperties<P extends { id: string }>(row: Page, shown: P[], presentation: CardSubitemPresentation): P[] {
  return presentation.mode === "card_property" && !(presentation.childCountByParent.get(row.id) ?? 0)
    ? shown.filter((item) => item.id !== presentation.childrenPropertyId) : shown;
}

/** A flattened list's parent card: how many sub-items it holds, after ↳ (Notion's, seen 2026-10-11). */
export function CardSubitemMeta({ row, presentation }: { row: Page; presentation: CardSubitemPresentation }) {
  const { t } = useTranslation("tableView");
  const count = presentation.mode === "flattened" && presentation.scope === "parents" ? presentation.childCountByParent.get(row.id) ?? 0 : 0;
  if (count <= 0) return null;
  return (
    <span className={styles.count} data-subitem-count={count} aria-label={t("tableView:subitemCount", { count })}>
      <SubitemCountIcon size="small" aria-hidden />
      <span>{count}</span>
    </span>
  );
}

/** A flattened list's sub-item card: its parent above its title, after ↰ (Notion's, seen 2026-10-11). */
export function CardSubitemParent({ row, pagesById, presentation }: {
  row: Page;
  pagesById: Record<string, Page>;
  presentation: CardSubitemPresentation;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("tableView");
  if (presentation.mode !== "flattened" || !row.subitemParentId) return null;
  const parent = pagesById[row.subitemParentId];
  const title = parent ? pageNames.title(parent) : "";
  return (
    <span className={styles.parent} data-subitem-parent-id={row.subitemParentId} title={t("tableView:subitemOf", { title })}>
      <SubitemParentIcon size="small" aria-hidden />
      <span>{title}</span>
    </span>
  );
}

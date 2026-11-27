import { useState } from "react";
import { useTranslation } from "react-i18next";
import { DragHandleIcon } from "@/icons/leafen";
import type { GraphProperty } from "@/contentGraph";
import type { PageLayoutModule } from "@/lib/databasePageLayout";
import { popoverRectOf, type PopoverRect } from "@/lib/popoverPlacement";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { PropertyIcon } from "./PropertyIcon";
import { RowPagePropertyMenu } from "./RowPagePropertyMenu";
import { usePageLayoutDrag } from "./usePageLayoutDrag";
import popoverStyles from "./PageLayoutCardPopover.module.css";

type RelationsModule = Extract<PageLayoutModule, { kind: "relations" }>;

/**
 * A relations group's card, as Notion's (seen 2026-11-07): its name, what
 * the group is for, then its relations, each by its property's mark (the
 * type's ↗ at 19.2, Notion's arrow-northeast mask, seen 2026-11-22 at 1440
 * dark, dpr 1 — not the ↗ of its menus). A relation's mark turns to "⠿" as
 * the pointer is on it, to drag it among the group's; pressing it opens
 * its menu by it — "표시 옵션 ›" (페이지 섹션으로 표시 or 최소화), its
 * name and settings, "레이아웃에 추가" and "속성 삭제".
 */
export function PageLayoutRelationsCard({ module, properties, editing, onDisplay, onMove, onTakeOut, onDelete }: {
  module: RelationsModule;
  properties: readonly GraphProperty[];
  /** The builder's own: a relation's new name or settings wait for "모든 페이지에 적용". */
  editing?: DatabasePropertyEditing;
  onDisplay(propertyId: string, display: "minimized" | "section"): void;
  onMove(from: string, to: string, after: boolean): void;
  /** "레이아웃에 추가": the relation a module of its own. */
  onTakeOut(propertyId: string): void;
  /** "속성 삭제". */
  onDelete?: (propertyId: string) => void;
}) {
  const { t } = useTranslation(["pageLayout"]);
  const [menu, setMenu] = useState<{ propertyId: string; anchor: PopoverRect } | null>(null);
  const drag = usePageLayoutDrag(module.relations.map(({ propertyId }) => propertyId), onMove);
  const open = (propertyId: string, row: HTMLElement | null) => {
    const anchor = row && popoverRectOf(row);
    if (anchor) setMenu({ propertyId, anchor });
  };
  const chosen = menu && properties.find(({ id }) => id === menu.propertyId);
  const chosenRelation = menu && module.relations.find(({ propertyId }) => propertyId === menu.propertyId);
  return <div className={popoverStyles.relationsCard} data-layout-relations-card>
    <div className={popoverStyles.head}><span>{t("modules.relations")}</span></div>
    <p className={popoverStyles.relationsHint}>{t("relationsHint")}</p>
    {module.relations.map((relation) => {
      const property = properties.find(({ id }) => id === relation.propertyId);
      if (!property) return null;
      const shown = menu?.propertyId === relation.propertyId;
      return <div key={relation.propertyId} className={popoverStyles.relationRow} data-layout-relation={relation.propertyId}
        data-open={shown || undefined} {...drag.target(relation.propertyId)}>
        <button type="button" className={popoverStyles.action} aria-haspopup="menu" aria-expanded={shown}
          onClick={(event) => open(relation.propertyId, event.currentTarget)}>
          <PropertyIcon property={property} size="regular" /><span>{property.name}</span>
        </button>
        <span role="button" tabIndex={-1} className={popoverStyles.relationHandle} aria-label={t("dragRow", { name: property.name })}
          {...drag.handle(relation.propertyId)} onClick={(event) => open(relation.propertyId, event.currentTarget.parentElement)}>
          <DragHandleIcon size="tiny" aria-hidden="true" />
        </span>
      </div>;
    })}
    {menu && chosen && chosenRelation ? (
      <RowPagePropertyMenu property={chosen} anchor={menu.anchor} editing={editing} onClose={() => setMenu(null)}
        layoutRelation={{
          display: chosenRelation.display,
          onDisplay: (display) => onDisplay(chosen.id, display),
          onAddToLayout: () => onTakeOut(chosen.id),
          onDelete: onDelete ? () => onDelete(chosen.id) : undefined,
        }} />
    ) : null}
  </div>;
}

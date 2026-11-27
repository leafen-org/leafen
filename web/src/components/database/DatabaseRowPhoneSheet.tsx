import { useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import {
  ArchiveIcon, ArrowBendUpRightIcon, BulletedListIcon, CommentIcon, Copy, LinkIcon, SmileIcon, Star, StarFilled, Trash,
} from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import sheetStyles from "../RowMenuPhoneSheet.module.css";
import { PropertyIcon } from "./PropertyIcon";
import type { DatabaseRowMenuLines } from "./useDatabaseRowViewMenu";

export type DatabaseRowPhoneSheetActions = {
  busy: boolean;
  readOnly: boolean;
  favorite?: { on: boolean; onToggle(): void };
  lines?: DatabaseRowMenuLines;
  copyLinkLabel?: string;
  onCopyLink(): void;
  onDuplicate?: () => void;
  /** "옮기기": the pages' move where the row is read or in a view, else its places in a sheet (DatabaseRowMovePhoneList). */
  move?: { onPress(): void } | { sheet(close: () => void): ReactNode };
  /** Leafen's own "보관", by the trash. */
  archive?: { label: string; onToggle(): void };
  onTrash?: () => void;
  /** Who edited it last, at the sheet's foot. */
  foot?: ReactNode;
};

/**
 * A row's menu on a phone — a table's or a list's row long pressed — as
 * Notion's "작업" sheet (seen 2026-11-25 on a Galaxy A90 in Chrome at 411, in
 * two tabs, from a table's title and text cells and a list's row alike): its
 * title 16px/600, where a page's "…" sheet has 17px/700 (RowMenuPhoneSheet's
 * `titleLarge`); under "페이지" favorite, icon, properties ›; comment; link,
 * duplicate, move, trash; who edited it last and the page's counts at its
 * foot. No key hints and no "다음에서 열기", the desktop menu's (DatabaseRowMenu);
 * Leafen's archive sits by the trash as the sidebar's sheet has it.
 * "속성 편집 ›" lists the properties over it in a whole sheet with "‹", a
 * property each, which opens that value by the row. The row it was opened on
 * is not chosen, open or closed (TableView, cardSelection's menuChoose).
 */
export function DatabaseRowPhoneSheet({ actions, onClose }: { actions: DatabaseRowPhoneSheetActions; onClose(): void }) {
  const { t } = useTranslation(["rowMenu", "topBar", "common"]);
  const [opened, setOpened] = useState<"properties" | "move" | null>(null);
  const { busy, readOnly, lines, move } = actions;
  const press = (run: () => void) => () => { if (!busy) run(); };
  // The lines that open by the row close the sheet first, as the desktop menu's do.
  const later = (run: () => void) => press(() => { onClose(); run(); });
  const properties = !readOnly && lines?.editProperties && lines.editProperties.properties.length > 0 ? lines.editProperties : undefined;
  const back = () => setOpened(null);
  return (
    <PhoneSheet title={t("rowMenu:phone.title")} onClose={onClose} data={{ "data-row-phone-actions": "" }}
      onKeyDown={(event) => { if (event.key === "Escape" && !opened) { event.stopPropagation(); onClose(); } }}>
      {actions.favorite || (lines?.onEditIcon && !readOnly) || properties ? (
        <PhoneSheetGroup label={t("rowMenu:phone.page")}>
          {actions.favorite ? (
            <PhoneSheetRow more={false} icon={actions.favorite.on ? <StarFilled size="regular" /> : <Star size="regular" />} disabled={readOnly}
              label={t(actions.favorite.on ? "rowMenu:actions.removeFromFavorites" : "rowMenu:actions.addToFavorites")} onPress={press(actions.favorite.onToggle)} />
          ) : null}
          {lines?.onEditIcon && !readOnly ? (
            <PhoneSheetRow more={false} icon={<SmileIcon size="regular" />} label={t("rowMenu:actions.editIcon")} onPress={later(lines.onEditIcon)}
              data={{ "data-row-edit-icon": "" }} />
          ) : null}
          {properties ? (
            <PhoneSheetRow icon={<BulletedListIcon size="regular" />} label={t("rowMenu:actions.editProperties")} disabled={busy}
              onPress={() => setOpened("properties")} data={{ "data-row-menu-beside": "properties" }} />
          ) : null}
        </PhoneSheetGroup>
      ) : null}
      {lines?.onComment ? (
        <PhoneSheetGroup>
          <PhoneSheetRow more={false} icon={<CommentIcon size="regular" />} label={t("topBar:comment")} onPress={later(lines.onComment)}
            data={{ "data-row-comment-action": "" }} />
        </PhoneSheetGroup>
      ) : null}
      <PhoneSheetGroup>
        <PhoneSheetRow more={false} icon={<LinkIcon size="regular" />} label={actions.copyLinkLabel ?? t("rowMenu:actions.copyLink")} onPress={press(actions.onCopyLink)} />
        {actions.onDuplicate ? (
          <PhoneSheetRow more={false} icon={<Copy size="regular" />} label={t("rowMenu:actions.duplicate")} disabled={readOnly} onPress={press(actions.onDuplicate)} />
        ) : null}
        {move ? (
          <PhoneSheetRow more={false} icon={<ArrowBendUpRightIcon size="regular" />} label={t("rowMenu:actions.moveTo")} disabled={readOnly}
            onPress={"sheet" in move ? () => setOpened("move") : press(move.onPress)} data={{ "data-page-move-action": "" }} />
        ) : null}
        {actions.archive ? (
          <PhoneSheetRow more={false} icon={<ArchiveIcon size="regular" />} label={actions.archive.label} disabled={readOnly} onPress={press(actions.archive.onToggle)} />
        ) : null}
        {actions.onTrash ? (
          <PhoneSheetRow more={false} danger icon={<Trash size="regular" />} label={t("rowMenu:actions.moveToTrash")} disabled={readOnly} onPress={press(actions.onTrash)}
            data={{ "data-row-trash-action": "" }} />
        ) : null}
      </PhoneSheetGroup>
      {actions.foot ? <PhoneSheetGroup className={sheetStyles.foot}>{actions.foot}</PhoneSheetGroup> : null}

      {opened === "properties" && properties ? (
        <PhoneSheet title={t("rowMenu:actions.editProperties")} onClose={onClose} onBack={back}>
          <PhoneSheetGroup>
            {properties.properties.map((property) => (
              <PhoneSheetRow key={property.id} more={false} icon={<PropertyIcon property={property} size="regular" />} label={property.name}
                onPress={later(() => properties.onEdit(property.id))} data={{ "data-row-edit-property": property.id }} />
            ))}
          </PhoneSheetGroup>
        </PhoneSheet>
      ) : null}
      {opened === "move" && move && "sheet" in move ? (
        // Whole with "완료" closing it all, as the page sheet's (RowMenuPhoneSheet).
        <PhoneSheet title={t("rowMenu:actions.moveTo")} onClose={onClose} action={{ label: t("common:actions.done"), onPress: onClose }}>{move.sheet(back)}</PhoneSheet>
      ) : null}
    </PhoneSheet>
  );
}

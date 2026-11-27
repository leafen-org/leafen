import { type ReactNode, useContext, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { GraphView } from "@/contentGraph";
import { ArrowDown, ArrowLeft, ArrowRight, ArrowsDownUpIcon, ArrowUp, ArrowUpRightIcon, ChevronRight, Copy, MenuIcon, NewRowAboveIcon, NewRowBelowIcon, SlidersIcon, Trash } from "@/icons/leafen";
import { useHoverTip } from "@/components/IconTipButton";
import { sidebarViewDimmed } from "@/components/sidebar/useViewToSidebar";
import type { DashboardWidgetMove } from "@/lib/dashboardWidgetMoves";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, placeBeside, popoverPlacementStyle, popoverRectOf, type PopoverRect, usePopoverPlacement } from "@/lib/popoverPlacement";
import { readsRightToLeft } from "@/lib/readingDirection";
import { useRouter } from "@/lib/router";
import { DatabaseDataSourcesContext } from "./databaseDataSourcesContext";
import { useDatabaseFullPageLink } from "./databaseFullPageLink";
import { databaseViewLabels } from "./databaseViewLabels";
import styles from "./DatabaseDashboardWidgets.module.css";

type Action = "source" | "edit" | "left" | "right" | "rows" | "sidebar" | "duplicate" | "remove";

/**
 * Where a widget's "데이터 소스 보기" goes: a linked source's own database, or
 * this database alone at its view (Notion's, seen 2026-11-15: its own
 * source's widget opened the database's page at the dashboard); null where
 * neither has an address.
 */
export function useDashboardSourceLinks() {
  const sources = useContext(DatabaseDataSourcesContext), ownHref = useDatabaseFullPageLink();
  return (widget: GraphView) => {
    const linked = sources?.linked.find(({ dataSourceId }) => dataSourceId === widget.dataSourceId);
    return linked ? linked.href ?? null : ownHref;
  };
}

/**
 * A widget's options under its name, as Notion's (seen 2026-11-15 in the
 * Business trial, in Edit mode and out of it): "데이터 소스 보기" first,
 * opening its source's database — a linked one's own, else this database
 * alone at its view — then, in Edit mode, "보기 편집"; "왼쪽으로 이동" /
 * "오른쪽으로 이동" while a neighbour stands that side and "행으로 이동"
 * opening the rows it can join and a new row of its own above or below while
 * its row holds another (dashboardWidgetMoves); "사이드바에 보기 추가"; and
 * "복제", "삭제", a line between each group. Outside Edit mode it offers the
 * source and "사이드바에 보기 추가" alone (`viewing`), the latter dimmed in both
 * for a widget a view tab's menu dims it for, its tip saying why
 * (sidebarViewDimmed).
 */
export function DashboardWidgetMenu({ widget, anchor, moves, viewing, onEdit, onAddToSidebar, onMove, onDuplicate, onRemove, onClose }: {
  widget: GraphView;
  /** The widget's name it opened from. */
  anchor: PopoverRect;
  /** The moves it can make now (dashboardWidgetMoves). */
  moves: readonly DashboardWidgetMove[];
  /** Outside Edit mode: its source and adding it to the sidebar. */
  viewing?: boolean;
  onEdit?(): void;
  onAddToSidebar?(): void;
  onMove(move: DashboardWidgetMove): void;
  onDuplicate(): void;
  onRemove(): void;
  onClose(): void;
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("databaseView");
  const router = useRouter();
  const sourceHref = useDashboardSourceLinks()(widget);
  const menuRef = useRef<HTMLDivElement>(null), rowsRef = useRef<HTMLButtonElement>(null);
  const [rowsOpen, setRowsOpen] = useState(false);
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeAround(anchor, size, viewport, { gap: 0 }),
    [anchor.left, anchor.bottom], undefined, { kind: "menu", onEscape: onClose, onOutsidePress: onClose });
  const dimmed = sidebarViewDimmed(widget, labels), reason = useHoverTip({ delay: 180, placement: "left" });
  // Left and right on the screen: a step back along the row where it reads from left to right.
  const leftStep = readsRightToLeft() ? 1 : -1;
  const along = (step: -1 | 1) => moves.find((move) => move.kind === "along" && move.step === step);
  const rowMoves = moves.filter((move) => move.kind !== "along");
  const groups: Action[][] = [
    sourceHref ? ["source"] : [],
    viewing || !onEdit ? [] : ["edit"],
    viewing ? [] : [...(along(leftStep as -1 | 1) ? ["left" as const] : []), ...(along(-leftStep as -1 | 1) ? ["right" as const] : []),
      ...(rowMoves.length ? ["rows" as const] : [])],
    onAddToSidebar ? ["sidebar"] : [],
    viewing ? [] : ["duplicate", "remove"],
  ].filter((group) => group.length > 0) as Action[][];
  const actions = groups.flat();
  const text: Record<Action, string> = { source: labels.viewDataSource, edit: labels.editView, left: t("moveDashboardWidgetLeft"),
    right: t("moveDashboardWidgetRight"), rows: labels.moveDashboardWidgetToRow, sidebar: labels.addViewToSidebar,
    duplicate: labels.duplicateDashboardWidget, remove: labels.deleteDashboardWidget };
  const icon: Record<Action, ReactNode> = { source: <ArrowUpRightIcon size="regular" />, edit: <SlidersIcon size="regular" />,
    left: <ArrowLeft size="regular" />, right: <ArrowRight size="regular" />, rows: <ArrowsDownUpIcon size="regular" />,
    sidebar: <MenuIcon size="regular" />, duplicate: <Copy size="regular" />, remove: <Trash size="regular" /> };
  const picker = useNativeMenu({ items: actions, open: true, menuRef, label: labels.dashboardWidgetMenu(widget.name),
    disabledKeys: dimmed.disabled ? ["sidebar"] : [], keyOf: (action) => action, textOf: (action) => text[action], onAction: (action) => {
      if (action === "rows") { setRowsOpen(true); return; }
      if (action === "source") { if (sourceHref) router.push(sourceHref); }
      else if (action === "edit") onEdit?.(); else if (action === "sidebar") onAddToSidebar?.();
      else if (action === "left" || action === "right") onMove(along((action === "left" ? leftStep : -leftStep) as -1 | 1)!);
      else if (action === "duplicate") onDuplicate(); else onRemove();
      onClose();
    } });
  return createPortal(<>
    <div {...picker.menuProps} ref={menuRef} className={styles.menu} style={popoverPlacementStyle(place)}
      data-dashboard-widget-menu={widget.id}>
      {groups.map((group) => (
        <div key={group[0]} className={styles.menuGroup}>{group.map((action) => (
          <NativeMenuItem key={action} className={styles.menuItem} state={picker.state} itemKey={action} buttonRef={action === "rows" ? rowsRef : undefined}
            aria-haspopup={action === "rows" ? "menu" : undefined} aria-expanded={action === "rows" ? rowsOpen : undefined}
            data-dashboard-widget-action={action}
            data-dashboard-widget-duplicate={action === "duplicate" ? widget.id : undefined}
            data-dashboard-widget-remove={action === "remove" ? widget.id : undefined}
            disabled={action === "sidebar" && dimmed.disabled}
            onPointerEnter={action === "rows" ? () => setRowsOpen(true)
              : action === "sidebar" && dimmed.reason ? (event) => reason.enter(event.currentTarget, dimmed.reason!) : undefined}
            onPointerLeave={action === "sidebar" && dimmed.reason ? reason.leave : undefined}>
            {({ labelProps }) => <>
              {icon[action]}
              <span {...labelProps}>{text[action]}</span>
              {action === "rows" ? <ChevronRight size="small" className={styles.menuChevron} /> : null}
            </>}
          </NativeMenuItem>
        ))}</div>
      ))}
      {reason.tip}
    </div>
    {rowsOpen && <RowsMenu anchor={popoverRectOf(rowsRef.current)} moves={rowMoves}
      onMove={(move) => { onMove(move); onClose(); }} onClose={() => setRowsOpen(false)} />}
  </>, document.body);
}

// Beside its row, its top on the row's and its side on the row's end, over the menu's 4px edge (Notion's, seen 2026-11-15).
function RowsMenu({ anchor, moves, onMove, onClose }: {
  anchor: PopoverRect | null; moves: readonly DashboardWidgetMove[]; onMove(move: DashboardWidgetMove): void; onClose(): void;
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("databaseView");
  const menuRef = useRef<HTMLDivElement>(null);
  const place = usePopoverPlacement(menuRef, anchor ? (size, viewport) => placeBeside(anchor, size, viewport, { gap: 0 }) : null,
    [anchor?.left, anchor?.top], undefined, { kind: "submenu", restoreFocus: false, closesHost: false, onEscape: onClose });
  const keyOf = (move: DashboardWidgetMove) => `${move.kind}:${move.step}`;
  const textOf = (move: DashboardWidgetMove) => move.kind === "row"
    ? move.step < 0 ? labels.moveDashboardWidgetUp : labels.moveDashboardWidgetDown
    : t(move.step < 0 ? "newDashboardRowAbove" : "newDashboardRowBelow");
  const picker = useNativeMenu({ items: [...moves], open: true, menuRef, label: labels.moveDashboardWidgetToRow,
    keyOf, textOf, onAction: onMove });
  return (
    <div {...picker.menuProps} ref={menuRef} className={`${styles.menu} ${styles.rowsMenu}`} style={popoverPlacementStyle(place)}>
      {moves.map((move) => (
        <NativeMenuItem key={keyOf(move)} className={styles.menuItem} state={picker.state} itemKey={keyOf(move)}
          data-dashboard-widget-move-row={move.kind === "row" ? move.step : undefined}
          data-dashboard-widget-new-row={move.kind === "newRow" ? move.step : undefined}>
          {({ labelProps }) => <>
            {move.kind === "row" ? move.step < 0 ? <ArrowUp size="regular" /> : <ArrowDown size="regular" />
              : move.step < 0 ? <NewRowAboveIcon size="regular" /> : <NewRowBelowIcon size="regular" />}
            <span {...labelProps}>{textOf(move)}</span>
          </>}
        </NativeMenuItem>
      ))}
    </div>
  );
}

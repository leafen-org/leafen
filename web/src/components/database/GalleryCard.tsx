import { memo, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { FileBlank } from "@/icons/leafen";
import { CardPropertySlots, fromWithinCard } from "./CardPropertySlots";
import type { PropertyCellEnvironment } from "./propertyCellTypes";
import { CardChoiceHalo } from "./cardSelection";
import { RowCommentCount } from "./RowCommentCount";
import type { DbProperty, Page } from "@/lib/types";
import { CardSubitemMeta, CardSubitemParent, rowCardProperties } from "./CardSubitemMeta";
import { CardPreviewMedia, type PageContentPreviewTransport, type WorkspaceFileHook } from "./CardPreviewMedia";
import { CardValueTip } from "./CardValueTip";
import { CardValuePress } from "./CardValuePress";
import { PropValue, type PropertyValueReadContext } from "./PropValue";
import type { CardViewSubitemPresentation } from "./queryProjection";
import { CardTitleIcon } from "./CardTitleIcon";
import { CardRowTitle } from "./CardRowTitle";
import { conditionalColorBackground } from "./conditionalColorModel";
import type { ColorName } from "./colors";
import styles from "./databaseSheet";
import { PeekHalo } from "./PeekHalo";
import { useCardFullLine } from "./cardFullLine";
import { sameOwnRowValues } from "./memoizedViewItems";
import type { GalleryRowOpenMode } from "./GalleryView";

/** What every card of a gallery draws alike, the same object while none of it changes (useSteadyFrame). */
export type GalleryCardFrame = {
  props: DbProperty[];
  /** The values its cards show, the title's aside. */
  others: DbProperty[];
  coverProperty?: string;
  showPreview: boolean;
  showTitle: boolean;
  fitImage: boolean;
  wrap: boolean;
  /** "페이지 아이콘 표시": a page's own icon before its title. */
  pageIcons: boolean;
  /** Its titles are not edited: a view read only, or a database without a title. */
  readOnly: boolean;
  /** Edits a value in place: one pressed, or its property slots while its name is typed. */
  environment?: PropertyCellEnvironment;
  useWorkspaceFile: WorkspaceFileHook;
  previewTransport: PageContentPreviewTransport;
  /** How its sub-items show; each card's own count is its `childCount`. */
  subitems: Pick<CardViewSubitemPresentation, "childrenPropertyId" | "mode" | "scope">;
  contextIdentity: string;
};

/** What a card's presses do, by the gallery as last drawn (useLiveWays). */
export type GalleryCardWays = {
  open(rowId: string, mode?: GalleryRowOpenMode): void;
  openClick(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  openKey(rowId: string, event: ReactKeyboardEvent<HTMLElement>): void;
  contextMenu(rowId: string, event: ReactMouseEvent<HTMLElement>): void;
  menu(rowId: string, element: HTMLElement): void;
  commitTitle(rowId: string, title: string): Promise<unknown>;
};

type GalleryCardProps = {
  row: Page;
  /** Its title as shown, and as ⌘C copies it when chosen (cardSelection.tsx). */
  title: string;
  /** The conditional color the card takes whole, if one matches. */
  tint: ColorName | undefined;
  /** How many sub-items it holds, where its gallery counts them. */
  childCount: number;
  /** Its parent's page, a flattened gallery's sub-item's. */
  parent: Page | undefined;
  /** Moves it by dragging it, in a view that may (useDragReorder's row). */
  rowRef?: (element: HTMLElement | null) => void;
  dragging?: string;
  dropSide?: string;
  frame: GalleryCardFrame;
  context: PropertyValueReadContext;
  ways: GalleryCardWays;
  /** Its line, which the gallery chooses: a row under several groups has one in each (viewGroupLines.ts). */
  line?: string;
};

/**
 * One card of a gallery, drawn again only when what it shows changed (its
 * page, title, color, sub-items, drag place, or the gallery's frame); the
 * value context, made anew as any row comes or goes, counts only where a value
 * it shows reads other rows (sameOwnRowValues).
 */
export const GalleryCard = memo(function GalleryCard({ row, title, tint, childCount, parent, rowRef, dragging, dropSide, frame, context, ways, line }: GalleryCardProps) {
  const { t } = useTranslation("galleryView");
  const { readOnly, environment: cellEnvironment, others, showTitle } = frame;
  const presentation = useMemo(() => ({ ...frame.subitems, childCountByParent: new Map(childCount ? [[row.id, childCount]] : []) }),
    [childCount, frame.subitems, row.id]);
  const parents = useMemo(() => parent ? { [parent.id]: parent } : {}, [parent]);
  const shown = rowCardProperties(row, others, presentation);
  const fullLine = useCardFullLine();
  const showSubitemMeta = presentation.mode === "flattened" && presentation.scope === "parents" && childCount > 0;
  const showMeta = shown.length > 0 || showSubitemMeta;
  const showBody = showTitle || showMeta;
  // Its name being typed, it shows its property slots, as the board's card (Notion's gallery, seen 2026-11-06).
  const [titleEditing, setTitleEditing] = useState(false);
  const slotHold = useRef(false);
  const onCommit = (value: string) => ways.commitTitle(row.id, value);
  const onMenu = (element: HTMLElement) => ways.menu(row.id, element);
  const onOpenPage = (pageId: string) => ways.open(pageId);

  return (
    <div
      ref={rowRef}
      data-dragging={dragging}
      data-drop-side={dropSide}
      data-native-drag-source={rowRef ? "" : undefined}
      className={styles.galleryCard}
      data-row-card
      data-row-id={row.id}
      data-card-choice={line ?? row.id}
      data-card-title={title}
      role="button"
      tabIndex={0}
      aria-label={t("galleryView:openRow", { title })}
      data-wrap={frame.wrap ? "true" : undefined}
      data-conditional-color={tint}
      style={tint ? { background: conditionalColorBackground(tint) } : undefined}
      onClick={(event) => { if (fromWithinCard(event)) ways.openClick(row.id, event); }}
      onKeyDown={(event) => { if (fromWithinCard(event)) ways.openKey(row.id, event); }}
      onAuxClick={(event) => {
        if (!fromWithinCard(event) || event.button !== 1) return;
        event.preventDefault();
        ways.open(row.id, "new_tab");
      }}
      onContextMenu={(event) => { if (fromWithinCard(event)) ways.contextMenu(row.id, event); }}
    >
      <PeekHalo itemId={row.id} />
      <CardChoiceHalo itemId={line ?? row.id} />
      {frame.showPreview && (
        <CardPreviewMedia
          row={row}
          props={frame.props}
          coverProperty={frame.coverProperty}
          fitImage={frame.fitImage}
          coverClassName={styles.galleryCover}
          iconClassName={styles.galleryIcon}
          iconSize={22}
          getPageContentPreview={frame.previewTransport}
          useWorkspaceFile={frame.useWorkspaceFile}
        />
      )}
      <CardSubitemParent row={row} pagesById={parents} presentation={presentation} />
      {!showBody && <CardRowTitle key={frame.contextIdentity} row={row} readOnly={readOnly} showTitle={false} onCommit={onCommit} onMenu={onMenu} />}
      {showBody && (
        <div className={styles.galleryBody}>
          <CardRowTitle key={frame.contextIdentity} row={row} readOnly={readOnly} showTitle={showTitle} className={styles.galleryTitle}
            icon={<CardTitleIcon row={row} show={frame.pageIcons} size={18} className={styles.cardTitleIcon} />}
            editingIcon={<span className={styles.galleryEditingIcon} aria-hidden="true"><FileBlank pixels={18} /></span>}
            onOpenSide={() => ways.open(row.id, "side")} onEditingChange={setTitleEditing} holdRef={slotHold}
            onCommit={onCommit} onMenu={onMenu} />
          {titleEditing && cellEnvironment && !readOnly && shown.length > 0 ? <CardPropertySlots className={styles.galleryMeta} cardProps={shown}
            context={context} page={row} rowId={row.id} environment={cellEnvironment} holdRef={slotHold} onClosed={() => undefined} />
          : showMeta && (
            <div className={styles.galleryMeta}>
              {shown.map((prop) => fullLine.has(prop.id) ? (
                <CardValueTip key={prop.id} name={prop.name}><CardValuePress className={styles.cardFullLine} data-card-full-line="true" line
                  property={prop} page={row} environment={readOnly ? undefined : cellEnvironment}>
                  <PropValue context={context} row={row} prop={prop} interactive={false} linksOpen checkboxName onOpenPage={onOpenPage} />
                </CardValuePress></CardValueTip>
              ) : (
                <CardValueTip key={prop.id} name={prop.name}><CardValuePress className={styles.cardLine} line property={prop} page={row}
                  environment={readOnly ? undefined : cellEnvironment}><PropValue context={context} row={row} prop={prop} interactive={false} linksOpen checkboxName onOpenPage={onOpenPage} /></CardValuePress></CardValueTip>
              ))}
              <CardSubitemMeta row={row} presentation={presentation} />
            </div>
          )}
          <RowCommentCount rowId={row.id} line inset={0} lineClassName={styles.galleryComments} />
        </div>
      )}
    </div>
  );
}, (previous, next) => {
  for (const key of Object.keys(next) as (keyof GalleryCardProps)[]) {
    if (key !== "context" && previous[key] !== next[key]) return false;
  }
  return sameOwnRowValues(previous.context, next.context, next.frame.others);
});

import { useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { placeAround, popoverPlacementStyle, popoverRectAt, usePopoverPlacement } from "@/lib/popoverPlacement";
import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { CustomEmojiAddDialog } from "../page/CustomEmojiAddDialog";
import { EmojiCatalogPanel } from "../page/EmojiCatalogPanel";
import type { SlashMenuAnchor } from "./SlashMenu";
import styles from "./InlineEmojiMenu.module.css";
import { useSidebarLayout } from "@/components/shell/AppShell";

const GAP = 4;
// It chooses its side as though 362 high, however few its emoji: under the
// line where that fits under it, else over it where that fits over it, else
// under it, as Notion's (measured 2026-11-12 in a 723px window, a 104px menu:
// under with 340 under and 364 over the line, over with 330 under and 374
// over; 2026-11-07 in a 583px one: under with 201 under, over with 197).
const SIDE_HEIGHT = 362;

/**
 * A line's ":" emoji menu, as Notion's (seen 2026-09-30): under the line,
 * the emoji whose names hold what follows ":", in their groups with the
 * group bar below and the first one marked. The arrow keys move the mark,
 * Enter puts the marked emoji in the ":" text's place and Escape closes the
 * menu, leaving the text; with no match it says so. The workspace's own emoji
 * come with them, and "이모지 추가" adds one (2026-10-16: they weren't found).
 * A few matches come alone, in rows under the line, with a "+" after them
 * (Notion's, measured 2026-11-02), or over it near the window's foot (11-07).
 */
export function InlineEmojiMenu({ anchor, query, custom, onPick, onClose }: {
  anchor?: SlashMenuAnchor;
  query: string;
  /** The workspace whose emoji the menu offers, and picking one. */
  custom?: { workspaceId: string; pageId: string; onPick(emoji: WorkspaceCustomEmoji): void; onHold?(held: boolean): void };
  onPick(emoji: string): void;
  onClose(): void;
}) {
  const { t } = useTranslation("slashMenu");
  const { phone } = useSidebarLayout(true);
  const menuRef = useRef<HTMLDivElement>(null);
  const [zIndex, setZIndex] = useState<number>();
  const [refreshKey, setRefreshKey] = useState(0);
  // Adding opens under its "+" or "추가", in the menu: the menu's mouse down
  // keeps the line's caret everywhere but in the dialog, whose name field takes it.
  const [addAnchor, setAddAnchor] = useState<HTMLElement | null>(null);
  const adding = addAnchor !== null;
  // Under the line or over it by its side height; a phone's by its line (popoverPlacement).
  const place = usePopoverPlacement(menuRef, anchor ? (size, viewport) => placeAround(
    popoverRectAt(anchor), size, viewport, { gap: GAP, lineSide: SIDE_HEIGHT, ...(phone ? {} : { marginY: 0 }) },
  ) : null, [anchor, phone], undefined, false);
  if (typeof document === "undefined" || !anchor) return null;
  return createPortal(
    <div ref={menuRef} className={styles.menu} role="dialog" aria-label={t("inlineEmoji")} data-inline-emoji-menu
      style={{ ...(place ? popoverPlacementStyle(place) : { top: anchor.bottom + GAP, left: anchor.left }), zIndex }}
      // The line keeps the caret while the menu is used.
      onMouseDown={(event) => { if (!(event.target instanceof Element && event.target.closest("[data-custom-emoji-add]"))) event.preventDefault(); }}>
      {/* A click in the add dialog, outside the menu, doesn't close it. */}
      <EmojiCatalogPanel filter={query} offerRandom={false} workspaceId={custom?.workspaceId} onPick={onPick} onClose={() => { if (!adding) onClose(); }} caretLayerRef={menuRef} onLayerZIndexChange={setZIndex}
        custom={custom ? { workspaceId: custom.workspaceId, refreshKey, onPick: custom.onPick,
          onAdd: (opener) => { custom.onHold?.(true); setAddAnchor(opener); } } : undefined} />
      {addAnchor && custom ? (
        // One added here goes into the line.
        <CustomEmojiAddDialog anchor={addAnchor} uploadTarget={{ workspaceId: custom.workspaceId, pageId: custom.pageId }}
          onClose={() => { setAddAnchor(null); custom.onHold?.(false); }}
          onAdded={(emoji) => { setAddAnchor(null); custom.onHold?.(false); setRefreshKey((current) => current + 1); custom.onPick(emoji); }} />
      ) : null}
    </div>,
    document.body,
  );
}

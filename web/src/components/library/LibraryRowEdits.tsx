import { useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { AnchoredIconPicker } from "@/components/page/AnchoredEmojiPicker";
import { hasExplicitPageIcon } from "@/components/page/PageIcon";
import type { PagePresentationChange } from "@/components/page/pagePresentation";
import { useRenameTitleField } from "@/components/page/RenameTitleField";
import { pageTitleKept } from "@/lib/typedDraftSender";
import { untitledPageDisplayTitle } from "@/lib/pageTitle";
import { useProductToast } from "@/lib/productToast";
import type { TextSpan } from "@/lib/types";
import type { Page } from "@/types";
import type { LibraryPageActions } from "./LibrarySelectionBar";
import styles from "./LibraryRowEdits.module.css";

type Box = { top: number; left: number; width: number; height: number };

/** The icon picker's width (PageIconPicker.module.css). */
const PICKER_WIDTH = 408;

/**
 * A library row's name editor, as Notion's library (seen 2026-11-09): pressing
 * the name opens a box over its cell, 1px out at its top and start, the caret
 * after its name (2026-11-11), so typing adds to it; Enter, Escape or a press outside closes it
 * keeping what was typed (Notion's Escape kept it too), Enter telling its
 * owner (onClose's key). The name keeps its mentions, as a rename box does
 * (useRenameTitleField).
 */
function LibraryNameEditor({ page, cell, onTitleChange, onClose }: {
  page: Page;
  cell: Box;
  onTitleChange: (title: string, spans?: TextSpan[]) => void;
  onClose: (key?: "Enter" | "Escape") => void;
}) {
  const { t } = useTranslation("pageHeader");
  const rename = useRenameTitleField({ title: page.title, spans: page.titleSpans, placeholder: untitledPageDisplayTitle(page),
    className: styles.input, caret: "end", kept: page.kind === "database" ? undefined : pageTitleKept(page.id, () => page), onTitleChange, onDone: onClose });

  function close() {
    rename.finish();
    onClose();
  }

  return createPortal(
    <>
      <button type="button" className={styles.backdrop} tabIndex={-1} aria-label={t("renamePage")} onClick={close} />
      <div className={styles.editor} role="dialog" aria-label={t("renamePage")} data-library-name-editor
        style={{ top: cell.top - 1, left: cell.left - 1, width: cell.width + 1 }}>
        {rename.field}
      </div>
    </>,
    document.body,
  );
}

/**
 * What a library row's name cell does with its page, as Notion's library (seen
 * 2026-11-09): its name renames it and its icon opens the icon picker; the
 * row's "열기" opens it. A database's row takes no change here (as the chosen
 * rows' bar), so its name opens it.
 */
export function useLibraryRowEdits({ workspaceId, actions, rowPageIds, onEnter }: {
  workspaceId: string;
  actions: LibraryPageActions;
  /** The database rows' pages listed, which take no change here. */
  rowPageIds: { has(pageId: string): boolean };
  /** A name closed with Enter, whose next row's name cell Notion's library chooses (useLibraryCellChoice). */
  onEnter?: (page: Page) => void;
}): {
  editName: (page: Page, cell: Box) => boolean;
  editIcon: (page: Page, at: Box) => boolean;
  overlays: ReactNode;
} {
  const { t } = useTranslation("rowMenu");
  const { notify } = useProductToast();
  const [name, setName] = useState<{ page: Page; cell: Box } | null>(null);
  const [icon, setIcon] = useState<{ page: Page; at: Box } | null>(null);

  function changeIcon(page: Page, change: PagePresentationChange) {
    setIcon(null);
    void actions.changePageMetadata(page, change).then((result) => {
      if (result === false) throw new Error("Icon change failed.");
      if (typeof result === "object") actions.onPageChange(result);
    }).catch(() => notify(t("toast.couldntChangeIcon"), "error"));
  }

  const overlays = (
    <>
      {name ? (
        <LibraryNameEditor page={name.page} cell={name.cell}
          onTitleChange={(title, spans) => void actions.changePageTitle(name.page, title, undefined, spans)}
          onClose={(key) => { setName(null); if (key === "Enter") onEnter?.(name.page); }} />
      ) : null}
      {icon ? (
        // Centered under the icon, about 6px down, as Notion's library (seen
        // 2026-11-09: a 408-wide picker at 211 under an icon at 406..424).
        <AnchoredIconPicker anchor={{ top: icon.at.top, bottom: icon.at.top + icon.at.height + 3,
          left: icon.at.left + icon.at.width / 2 - PICKER_WIDTH / 2 }}
          marker="library-row-icon" onClose={() => setIcon(null)}
          picker={{
            current: icon.page,
            onPick: (emoji) => changeIcon(icon.page, { icon: emoji, iconType: "emoji" }),
            onPickCustomEmoji: (emoji) => changeIcon(icon.page, { icon: emoji.id, iconType: "custom_emoji" }),
            onPickBuiltInIcon: (value) => changeIcon(icon.page, { icon: value, iconType: "icon" }),
            onPickImage: (url) => changeIcon(icon.page, { icon: url, iconType: "image" }),
            onRemove: hasExplicitPageIcon(icon.page) ? () => changeIcon(icon.page, { icon: "", iconType: "none" }) : undefined,
            uploadTarget: { workspaceId, pageId: icon.page.id },
          }} />
      ) : null}
    </>
  );

  return {
    editName: (page, cell) => {
      if (rowPageIds.has(page.id)) return false;
      setName({ page, cell });
      return true;
    },
    editIcon: (page, at) => {
      if (rowPageIds.has(page.id)) return false;
      setIcon({ page, at });
      return true;
    },
    overlays,
  };
}

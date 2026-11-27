import type { MouseEvent as ReactMouseEvent, ReactNode } from "react";
import { PageIcon } from "@/components/page/PageIcon";
import { ArrowUpRightIcon, Pencil } from "@/icons/leafen";
import { usePageNames } from "@/lib/pageNames";
import { blockItemText } from "./blockItemLabels";
import { requestEditorBlockCommand } from "./editorCommands";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { linkedPage, openPressedLink } from "./linkOpening";
import styles from "./editorStyles.generated.module.css";

/**
 * A linked picture's press opens its link, in an editable page too, as
 * Notion's (seen 2026-11-12): another site's in a new tab, a page here in its
 * place, any with ⌘ or Ctrl held in a new tab. The block is chosen by a press
 * beside the picture, its handle or the keyboard, not on the picture.
 */
function press(href: string) {
  return (event: ReactMouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    openPressedLink(href, event.metaKey || event.ctrlKey);
  };
}

/** The picture itself, a link to `href`. */
export function ImageLinkAnchor({ href, children }: { href: string; children: ReactNode }) {
  return <a href={href} target="_blank" rel="noopener noreferrer" data-image-link onClick={press(href)}>{children}</a>;
}

/**
 * Where a linked picture leads, shown with the pointer on it, as Notion's
 * (seen 2026-11-12, editable and locked alike): the picture darkens, a shade
 * 88px high rises from its foot, and 12px in from its start, 8px up, the link
 * shows in 12px white — a page here by its icon, with a page link's arrow, and
 * its title; any other address by an arrow and its site, without "www." —
 * then, where the page can be changed, a pencil ("링크 편집") that opens the
 * picture's link window as ⌘K does.
 */
export function ImageLinkLabel({ adapter, blockId, href }: { adapter: NativeEditorGraphAdapter; blockId: string; href: string }) {
  const pageNames = usePageNames();
  const page = linkedPage(href, adapter.moveContext?.pages);
  return <div className={styles.imageLinkCover} contentEditable={false} data-image-link-label>
    <div className={styles.imageLinkShade} />
    <div className={styles.imageLinkLabel}>
      <a href={href} target="_blank" rel="noopener noreferrer" className={styles.imageLinkTarget} onClick={press(href)}>
        {page ? <PageIcon page={page} size={20} className={styles.imageLinkPageIcon} />
          : <span className={styles.imageLinkArrow}><ArrowUpRightIcon size="small" aria-hidden="true" /></span>}
        <span className={styles.imageLinkText}>{page ? pageNames.title(page) : linkSite(href)}</span>
      </a>
      {adapter.readOnly ? null : <button type="button" className={styles.imageLinkEdit} aria-label={blockItemText("image.editLink")}
        onClick={(event) => requestEditorBlockCommand(blockId, "imageLink", undefined, event.currentTarget)}>
        <Pencil size="small" aria-hidden="true" />
      </button>}
    </div>
  </div>;
}

/** A link's site as Notion's picture names it (seen 2026-11-12: "https://www.example.org/docs/…" → "example.org"). */
function linkSite(href: string) {
  try {
    const { hostname } = new URL(href);
    return hostname.replace(/^www\./u, "") || href;
  } catch {
    return href;
  }
}

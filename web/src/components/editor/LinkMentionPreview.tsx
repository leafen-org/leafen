import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { i18next } from "@/i18n";
import { DotsHorizontal, LinkIcon, RepeatIcon } from "@/icons/leafen";
import { copyText } from "@/lib/copyText";
import { hoverCardAnchor, listenHoverCardKeys } from "@/lib/hoverCard";
import { NativeMenuItem, useNativeMenu } from "@/lib/nativeMenu";
import { placeAround, placeBeside, popoverPlacementStyle, popoverRectOf, usePopoverPlacement, type PopoverRect } from "@/lib/popoverPlacement";
import { useOptionalProductToast } from "@/lib/productToast";
import { blockItemText } from "./blockItemLabels";
import { linkMentionTurn } from "./linkMention";
import styles from "./LinkMentionPreview.module.css";

const SHOW_AFTER_MS = 500;
const HIDE_AFTER_MS = 150;
const MENTION = 'a[data-mention="external"][href]';

type Shown = { element: HTMLAnchorElement; anchor: PopoverRect };
type Row = { key: "copy" | "preview"; label: string; run(): void };

/**
 * The card a mention of an outside page shows while the pointer rests on
 * it, as Notion's link mention (seen 2026-11-11, dark, 1975px): 6px under
 * the mention from its start (over it near the window's foot), 280px, the
 * page's picture 120px across its top, then 12px and 16px in its title
 * (14px/600, two lines), its description (12px, three lines) and the
 * site's icon and host (12px/500 grey, "www." left off) 12px under them.
 * A dark 24px "⋯" 8px in from its top end opens, 4px beside it, "링크 복사"
 * and — where the mention's text can be changed — "미리보기로 전환".
 */
export function LinkMentionPreview() {
  const [shown, setShown] = useState<Shown | null>(null);
  const [menu, setMenu] = useState<PopoverRect | null>(null);
  const cardRef = useRef<HTMLDivElement>(null), menuRef = useRef<HTMLDivElement>(null), moreRef = useRef<HTMLButtonElement>(null);
  const live = useRef({ shown, menu }); live.current = { shown, menu };
  const toast = useOptionalProductToast();

  useEffect(() => {
    let showTimer = 0, hideTimer = 0;
    const inside = (target: EventTarget | null) => target instanceof Node
      && Boolean(cardRef.current?.contains(target) || menuRef.current?.contains(target));
    const hide = () => { window.clearTimeout(showTimer); window.clearTimeout(hideTimer); setShown(null); setMenu(null); };
    function over(event: MouseEvent) {
      if (inside(event.target)) { window.clearTimeout(hideTimer); return; }
      const mention = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>(MENTION) : null;
      if (!mention) {
        window.clearTimeout(showTimer);
        if (live.current.shown && !live.current.menu) {
          window.clearTimeout(hideTimer);
          hideTimer = window.setTimeout(hide, HIDE_AFTER_MS);
        }
        return;
      }
      window.clearTimeout(hideTimer);
      if (live.current.shown?.element === mention) return;
      window.clearTimeout(showTimer);
      showTimer = window.setTimeout(() => {
        if (!mention.isConnected) return;
        setMenu(null);
        setShown({ element: mention, anchor: hoverCardAnchor(mention)! });
      }, SHOW_AFTER_MS);
    }
    // A press or a scroll elsewhere ends it, the card's own keep it; a key does not (listenHoverCardKeys), Escape in its
    // "⋯" choices closing them alone.
    const press = (event: Event) => { if (!inside(event.target)) hide(); };
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape" && live.current.menu) { event.stopPropagation(); setMenu(null); }
    };
    document.addEventListener("mouseover", over);
    document.addEventListener("mousedown", press, true);
    document.addEventListener("keydown", key, true);
    const keys = listenHoverCardKeys(() => live.current.shown?.element ?? null, hide);
    window.addEventListener("scroll", press, true);
    return () => {
      window.clearTimeout(showTimer); window.clearTimeout(hideTimer);
      document.removeEventListener("mouseover", over);
      document.removeEventListener("mousedown", press, true);
      document.removeEventListener("keydown", key, true);
      keys();
      window.removeEventListener("scroll", press, true);
    };
  }, []);

  const place = usePopoverPlacement(cardRef, shown ? (size, viewport) => placeAround(
    shown.anchor, size, viewport, { gap: 6, minHeight: size.height },
  ) : null, [shown], undefined, { kind: "hovercard" });
  if (!shown || typeof document === "undefined") return null;
  const element = shown.element, data = element.dataset;
  const href = element.getAttribute("href") ?? "";
  const title = data.title || element.querySelector('[data-mention-label="external"]')?.textContent || href;
  const turn = linkMentionTurn(element);
  const rows: Row[] = [{ key: "copy", label: blockItemText("linkPreview.copyLink"), run: () => void copyLink(href, toast) }];
  if (turn) rows.push({ key: "preview", label: blockItemText("linkPreview.turnIntoPreview"), run: turn });
  return createPortal(<>
    <div ref={cardRef} className={styles.card} role="tooltip" data-link-mention-preview="" style={popoverPlacementStyle(place)}>
      {data.imageUrl ? <img className={styles.picture} src={data.imageUrl} alt="" draggable={false} /> : null}
      <button ref={moreRef} type="button" className={styles.more} aria-label={blockItemText("linkPreview.more")} aria-haspopup="menu"
        aria-expanded={Boolean(menu)} onClick={() => setMenu(menu ? null : popoverRectOf(moreRef.current))}>
        <DotsHorizontal size="small" aria-hidden="true" />
      </button>
      <div className={styles.body}>
        <div className={styles.title}>{title}</div>
        {data.description ? <div className={styles.description}>{data.description}</div> : null}
        <div className={styles.site}>
          {data.iconUrl ? <img className={styles.siteIcon} src={data.iconUrl} alt="" draggable={false} />
            : <LinkIcon size="small" className={styles.siteLink} aria-hidden="true" />}
          <span className={styles.host}>{shownHost(href)}</span>
        </div>
      </div>
    </div>
    {menu ? <LinkMentionMenu at={menu} rows={rows} menuRef={menuRef} onDone={() => { setMenu(null); setShown(null); }} /> : null}
  </>, document.body);
}

/** The card's "⋯" choices, 180px, beside the button (Notion's, seen 2026-11-11). */
function LinkMentionMenu({ at, rows, menuRef, onDone }: {
  at: PopoverRect; rows: Row[]; menuRef: React.RefObject<HTMLDivElement | null>; onDone(): void;
}) {
  const place = usePopoverPlacement(menuRef, (size, viewport) => placeBeside(at, size, viewport, { gap: 4 }), [at], undefined,
    { kind: "submenu", restoreFocus: false, closesHost: false });
  const picker = useNativeMenu({ items: rows, open: true, menuRef, label: blockItemText("linkPreview.more"),
    keyOf: (row) => row.key, textOf: (row) => row.label, focusOnOpen: true, quietOnPointer: true,
    onAction: (row) => { onDone(); row.run(); } });
  return <div {...picker.menuProps} ref={menuRef} className={styles.menu} style={popoverPlacementStyle(place)} data-link-mention-menu="">
    {rows.map((row) => <NativeMenuItem key={row.key} state={picker.state} itemKey={row.key} className={styles.row} data-link-mention-row={row.key}>
      {({ labelProps }) => <>
        {row.key === "copy" ? <LinkIcon size="regular" aria-hidden="true" /> : <RepeatIcon size="regular" aria-hidden="true" />}
        <span {...labelProps}>{row.label}</span>
      </>}
    </NativeMenuItem>)}
  </div>;
}

/** Copies the mention's link as plain words, saying so as Notion's does ("링크 복사 완료", seen 2026-11-11). */
async function copyLink(href: string, toast: ReturnType<typeof useOptionalProductToast>) {
  const copied = await copyText(href);
  toast?.notify(i18next.t(copied ? "selectionToolbar:linkEditor.copiedLink" : "blockHandle:notifications.couldNotCopyLink"),
    copied ? "success" : "error");
}

/** The page's host as the card's foot names it, without "www." (Notion's "wikipedia.org"); a mail address or phone number itself. */
function shownHost(href: string) {
  try {
    return new URL(href).hostname.replace(/^www\./iu, "") || href.replace(/^(?:mailto|tel):/iu, "");
  } catch {
    return href;
  }
}

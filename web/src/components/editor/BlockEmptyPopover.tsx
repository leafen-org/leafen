import { type FormEvent, type ReactNode, useContext, useEffect, useId, useRef, useState } from "react";
import { useFileDrop } from "@/lib/fileDropTarget";
import { giveFocusBack } from "@/lib/focusGiveBack";
import { keyCameLast } from "@/lib/lastPress";
import { holdingTyping } from "@/lib/pageTitleFocus";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { popoverPlacementStyle, popoverRectOf, usePopoverPlacement } from "@/lib/popoverPlacement";
import { PopoverLayer } from "@/lib/popoverLayer";
import { phoneScreen } from "@/lib/screenKind";
import { externalDragHasFiles, FILE_DROP_BOUNDS, type BlockUploadProgress } from "./fileDrop";
import { type BlockSetupAsk, useBlockSetupRequest } from "./blockSetup";
import { focusBlockControl, focusNextFrame } from "./focus";
import { givenWebLink } from "./mediaBlockUrl";
import { blockItemText } from "./blockItemLabels";
import { confirmAction } from "../ConfirmActionDialog";
import { SearchClearButton } from "@/components/PageActionSearch";
import { phoneSheetStyles as sheetStyles } from "../shell/PhoneSheet";
import { BlockEmptyPhoneContext, BlockEmptyPhoneSheet } from "./BlockEmptyPhoneSheet";
import styles from "./BlockEmptyPopover.module.css";
import phoneStyles from "./BlockEmptyPhoneSheet.module.css";

/**
 * An empty media, link or HTML block as Notion draws one (seen 2026-10-18):
 * a gray row naming what to add, which opens a window centered under it —
 * over it when there is no room below — with its tabs (upload, link), the
 * file button or drop area, or the link field, a blue button and a hint.
 * Made from the "/" menu, the block opens its window on its first choice:
 * the first tab, or the link field when the window starts on a link
 * (2026-10-16: the blocks showed their whole form in a box instead). A
 * link-only window given a phone title is a whole screen on a phone
 * (BlockEmptyPhoneSheet).
 */

export type BlockEmptyTab = { id: string; label: string; icon?: ReactNode };

export function BlockEmptyPopover({
  blockId,
  name,
  icon,
  label,
  width = 300,
  tabs,
  children,
  rowHint,
  aside,
  variant,
  gap = 0,
  phoneTitle,
  phoneHalf = false,
}: {
  blockId: string;
  /** What the block is, for its data attribute ("image", "bookmark"). */
  name: string;
  icon: ReactNode;
  label: string;
  /** Notion's window: 540px for an image or HTML, 300px for the others. */
  width?: number;
  /** None for a window that only takes a link. */
  tabs?: BlockEmptyTab[];
  children: (tab: string) => ReactNode;
  /** Shown in the row under its words, as an upload's progress or a failure. */
  rowHint?: ReactNode;
  /** Held at the row's right, as a link's "알 수 없는 URL" (EmbedUnknownLink). */
  aside?: ReactNode;
  /** "unknown": a service's row given a link it can't draw, lined and in the text color, as Notion's. */
  variant?: "unknown";
  /** The room between the row and its window: Notion's file block's stands 8px off, the others' touch it. */
  gap?: number;
  /** Its name on a phone, where it opens as a whole screen, as Notion's "북마크" and "임베드" (BlockEmptyPhoneSheet). */
  phoneTitle?: string;
  /** On a phone, a half sheet rather than a whole screen, as Notion's "HTML". */
  phoneHalf?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState(tabs?.[0]?.id ?? "link");
  const rootRef = useRef<HTMLDivElement>(null);
  const rowRef = useRef<HTMLButtonElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const windowId = useId();
  // On a phone the screen is the sheet's own, its close and Esc too.
  const phoneSheet = Boolean(phoneTitle) && phoneScreen();
  // Esc closes it. Its window floats over the page, which takes no pointer under it (pageHoverShield.ts): a press on a
  // page line only closes it, no caret put there.
  // Opened by a press on its row, a window that starts on its upload (a picture's, file's, video's, audio's or HTML's)
  // gives the focus back to that row as Escape or a press beside it closes it, ringed once a key comes, as Notion's
  // (2026-11-26, Chrome 1440, judged after the closing by the screen and one typed letter: a press beside it — each of
  // the five, the picture's also from its link tab; Escape — the picture's, ringed at once). One that starts on its
  // link field (an embed's, a bookmark's) leaves it on nothing, as Notion's (the same day).
  // Opened from "/", the text block it was made from is gone. Chosen there with Enter, one that starts on its upload
  // sends the caret to the start of the page's title as Escape or a press beside it puts it away, the page left where
  // it is (focusTitleStartInPlace), as the database panel made from "/" does (DatabaseStartPanel); chosen by a press on
  // the menu's row, or starting on its link field, it leaves the caret nowhere. Notion's, measured 2026-11-26 in
  // Chrome at 1440 on a page made by hand, no script before the keys, one letter typed a second after: "/image",
  // "/video", "/file" and "/html" chosen with Enter, put away by Escape or a press on a page line, the letter went
  // before the title's first letter ten times out of eleven in four tabs, from far down the page too, the page coming
  // up with the letter (one "/html" from far down went nowhere); "/image" chosen by a press, put away by either,
  // nowhere twice (as an HTML window was seen 2026-11-15); "/embed" and "/bookmark" chosen with Enter, put away by
  // Escape or a press, nowhere.
  const openedByRow = useRef(false);
  const slashKey = useRef<BlockSetupAsk["slashKey"]>(undefined);
  const startsOnUpload = (tabs?.[0]?.id ?? "link") !== "link";
  const close = (pressed: boolean) => {
    setOpen(false);
    if (!startsOnUpload || phoneScreen()) return;
    if (openedByRow.current) giveFocusBack({ to: rowRef.current, keepScroll: true, unlessOpened: true, ring: keyCameLast });
    else if (slashKey.current) {
      const { pageId } = slashKey.current;
      // A frame on, once the window is gone; a press whose target took the focus keeps it.
      focusNextFrame(() => ({ titleStart: pageId }), () => pressed && Boolean(document.activeElement)
        && document.activeElement !== document.body && !holdingTyping());
    }
  };
  const { zIndex } = useOverlayLayer(open && !phoneSheet, { kind: "picker", ref: rootRef, onEscape: () => close(false), onOutsidePress: () => close(true),
    returnFocusRef: rowRef });
  // Opened, its field takes the caret, or its chosen tab where it starts on an upload.
  const focusOpened = (shown: string) => {
    if (!phoneSheet) focusBlockControl(blockId, shown !== "link" ? "[data-block-empty-tab][aria-selected='true']" : "[data-block-empty-link]");
  };
  // A link its service can't draw closes the window, the row saying so, as
  // Notion's (seen 2026-11-07: a Figma community file); the row keeps no ring.
  const shownVariant = useRef(variant);
  useEffect(() => {
    if (variant === "unknown" && shownVariant.current !== "unknown" && open) {
      setOpen(false);
      requestAnimationFrame(() => {
        if (document.activeElement === rowRef.current) rowRef.current?.blur();
      });
    }
    shownVariant.current = variant;
  }, [variant, open]);
  useBlockSetupRequest(blockId, (ask) => {
    openedByRow.current = false;
    slashKey.current = ask.slashKey;
    const first = tabs?.[0]?.id ?? "link";
    setTab(first);
    setOpen(true);
    focusOpened(first);
  });

  // Centered under the row, over it when only that side has room, and kept
  // in the window as every menu is: Notion's window is in its overlay, over
  // the sidebar, not cut by the page (2026-11-05: at 768 wide it ran under
  // the sidebar and its "업로드" tab read "로드"), so it is drawn on the
  // page's body, still the block's (PopoverLayer).
  const place = usePopoverPlacement(popoverRef, open && !phoneSheet ? () => ({
    anchor: popoverRectOf(rootRef.current) ?? { top: 0, bottom: 0, left: 0, right: 0 }, placement: "bottom", gap, options: { whole: true },
  }) : null, [open, tab, gap], undefined, false);

  function choose(next: string) {
    setTab(next);
    // The link tab puts the caret in its field, as Notion's; a phone's waits for a tap.
    if (next === "link" && !phoneSheet) focusBlockControl(blockId, "[data-block-empty-link]");
  }

  const tabButtons = tabs?.map((item) => (
    <button key={item.id} type="button" role="tab" className={styles.tab} aria-selected={item.id === tab}
      data-block-empty-tab={item.id} onClick={() => choose(item.id)}>
      {item.icon ? <span className={styles.tabIcon} aria-hidden="true">{item.icon}</span> : null}
      {item.label}
    </button>
  ));

  return (
    <div ref={rootRef} className={styles.root} contentEditable={false} data-block-empty={name} data-variant={variant}>
      <button ref={rowRef} type="button" className={styles.row} aria-expanded={open} aria-haspopup="dialog"
        aria-controls={open ? windowId : undefined} data-block-empty-row onClick={() => {
          // A click opens it as "/" does, the caret in its link (Notion's, seen 2026-11-06).
          if (open) { setOpen(false); return; }
          openedByRow.current = true;
          slashKey.current = undefined;
          setOpen(true);
          focusOpened(tab);
        }}>
        <span className={styles.rowIcon} aria-hidden="true">{icon}</span>
        <span className={styles.rowLabel} data-block-empty-label>{label}</span>
      </button>
      {rowHint}
      {aside ? <span className={styles.aside}>{aside}</span> : null}
      {open && phoneSheet ? (
        <BlockEmptyPhoneSheet name={name} title={phoneTitle!} half={phoneHalf} onClose={() => setOpen(false)}
          tabs={tabs ? <div className={styles.phoneTabs} role="tablist" data-half={phoneHalf ? "true" : undefined}>{tabButtons}</div> : null}>
          {children(tab)}
        </BlockEmptyPhoneSheet>
      ) : open ? <PopoverLayer>
        <div ref={popoverRef} id={windowId} className={styles.popover} role="dialog" aria-label={label}
          data-placement={place?.side === "above" ? "above" : "below"} data-block-empty-window
          style={{ ...popoverPlacementStyle(place), zIndex, "--block-empty-width": `${width}px` } as never}>
          {tabs ? <div className={styles.tabs} role="tablist">{tabButtons}</div> : null}
          {children(tab)}
        </div>
      </PopoverLayer> : null}
    </div>
  );
}

/** A link field, its blue button and the hint under it. `onLink` returns an error to show, or "". */
export function BlockEmptyLinkPane({ placeholder, submit, hint, initial = "", onLink }: {
  placeholder: string;
  submit: string;
  hint?: string;
  initial?: string;
  onLink: (url: string) => string;
}) {
  const [draft, setDraft] = useState(initial);
  const field = useRef<HTMLInputElement>(null);
  const phone = useContext(BlockEmptyPhoneContext);

  // An empty field's button is lit and does nothing, as Notion's (seen 2026-11-01).
  // The link goes on as given, "http://" put before one without its scheme
  // (givenWebLink); one that is no web link goes as typed, for its window's
  // own refusal. The browser's own check of a URL field, which stopped
  // "example.net" with its bubble, is off: Notion takes it (seen 2026-11-06).
  // A refusal is a notice over the page with "확인" alone, the field keeping
  // what was typed, as Notion's ("유효한 링크를 입력하세요.", seen 2026-11-06).
  // Answered, the focus goes back where it was when the notice opened (the
  // notice's own return): the field after Enter in it, the pressed button
  // after a press. Notion's, measured 2026-11-15 in Chrome at 1440 with no
  // script in the page before the action — after "확인", a typed "z" went
  // into the field after Enter and nowhere after pressing "링크 임베드", the
  // focus on that button.
  function commit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft.trim()) return;
    const failure = onLink(givenWebLink(draft) || draft.trim());
    if (!failure) return;
    void confirmAction({ message: failure, confirmLabel: blockItemText("common.ok"), cancelLabel: null });
  }

  if (phone) {
    return (
      <form className={phoneStyles.form} onSubmit={commit} data-block-empty-pane="link" noValidate>
        <div className={phoneStyles.row}>
          <input ref={field} className={phoneStyles.field} type="url" value={draft} placeholder={placeholder} aria-label={placeholder}
            data-block-empty-link data-filled={draft ? "true" : undefined} onChange={(event) => setDraft(event.target.value)} />
        </div>
        <div>
          <button type="submit" className={`${phoneStyles.row} ${phoneStyles.submit}`}>{submit}</button>
          {hint ? <div className={phoneStyles.hint}>{hint}</div> : null}
        </div>
      </form>
    );
  }
  return (
    <form className={styles.body} onSubmit={commit} data-block-empty-pane="link" noValidate>
      {/* A typed link clears from the field's end by Notion's shared "입력 값 지우기" (seen 2026-11-06, 2026-11-17). */}
      <div className={styles.fieldWrap}>
        <input ref={field} className={styles.field} type="url" value={draft} placeholder={placeholder} aria-label={placeholder}
          data-block-empty-link data-filled={draft ? "true" : undefined} onChange={(event) => setDraft(event.target.value)} />
        <SearchClearButton value={draft} inputRef={field} over onClear={() => setDraft("")} />
      </div>
      <button type="submit" className={styles.primary}>{submit}</button>
      {hint ? <div className={styles.hint}>{hint}</div> : null}
    </form>
  );
}

/**
 * The upload tab: Notion's blue "choose a file" button, for an image its
 * outlined "파일 업로드", or for an HTML file a dashed area — each takes a
 * file dropped on it for this block, not the page under the window (seen
 * 2026-11-07 in the app: a PDF let go on a file block's button made a second
 * block below it). Its progress and failures show under the block's row,
 * where a dropped file's show too.
 */
export function BlockEmptyUploadPane({ accept, choose, phoneChoose, drop, outlined, kind, progress, onFile }: {
  accept: string;
  choose: string;
  /** Its words on a phone, where they differ, as Notion's HTML "파일 선택하기". */
  phoneChoose?: string;
  /** The empty block's kind, whose window Notion draws to its own measure (video, audio, PDF). */
  kind?: string;
  drop?: { title: string; hint: string; icon: ReactNode };
  /** An outlined button the width of the window, as Notion's image "파일 업로드" (seen 2026-10-31), not the blue one. */
  outlined?: boolean;
  progress: BlockUploadProgress | null;
  onFile: (file: File | undefined) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const dropRef = useFileDrop((files) => onFile(files[0]), {
    canDrop: (types) => !progress && externalDragHasFiles(types, FILE_DROP_BOUNDS), onOver: setDragging });
  const phone = useContext(BlockEmptyPhoneContext);
  const fileInput = <input ref={inputRef} type="file" accept={accept} hidden tabIndex={-1} aria-hidden="true"
    onChange={(event) => { onFile(event.target.files?.[0]); event.target.value = ""; }} />;

  // On a phone, as Notion's (seen 2026-11-15 on a Galaxy A90 at 411): a
  // picture's outlined button across the screen, 28 under the tabs; the
  // others' blue row; an HTML file's blue row in a group of the half sheet.
  if (phone) {
    const pick = () => inputRef.current?.click();
    return (
      <div className={phoneStyles.form} data-block-empty-pane="upload" data-kind={kind}>
        {fileInput}
        {outlined ? (
          <button ref={dropRef} type="button" className={styles.outlined} data-block-empty-upload disabled={Boolean(progress)}
            aria-busy={Boolean(progress)} onClick={pick}>{choose}</button>
        ) : drop ? (
          <div className={sheetStyles.group}>
            <button ref={dropRef} type="button" className={`${sheetStyles.row} ${phoneStyles.groupChoose}`} data-block-empty-upload
              disabled={Boolean(progress)} aria-busy={Boolean(progress)} onClick={pick}>{phoneChoose ?? choose}</button>
          </div>
        ) : (
          <button ref={dropRef} type="button" className={`${phoneStyles.row} ${phoneStyles.submit}`} data-block-empty-upload
            disabled={Boolean(progress)} aria-busy={Boolean(progress)} onClick={pick}>{phoneChoose ?? choose}</button>
        )}
      </div>
    );
  }

  return (
    <div className={styles.body} data-block-empty-pane="upload" data-drop={drop ? "true" : undefined}
      data-outlined={outlined ? "true" : undefined} data-kind={kind}>
      {fileInput}
      {drop ? (
        <button ref={dropRef} type="button" className={styles.drop} data-dragging={dragging || undefined} data-block-empty-upload
          disabled={Boolean(progress)} onClick={() => inputRef.current?.click()}>
          <svg className={styles.dropEdge} aria-hidden="true">
            <rect x="1" y="1" width="calc(100% - 2px)" height="calc(100% - 2px)" rx="10" />
          </svg>
          <span className={styles.dropTitle}><span aria-hidden="true">{drop.icon}</span>{drop.title}</span>
          <span className={styles.dropHint}>{drop.hint}</span>
        </button>
      ) : outlined ? (
        <button ref={dropRef} type="button" className={styles.outlined} data-dragging={dragging || undefined} data-block-empty-upload
          disabled={Boolean(progress)} aria-busy={Boolean(progress)} onClick={() => inputRef.current?.click()}>{choose}</button>
      ) : (
        <button ref={dropRef} type="button" className={styles.primary} data-dragging={dragging || undefined} data-block-empty-upload
          disabled={Boolean(progress)} aria-busy={Boolean(progress)} onClick={() => inputRef.current?.click()}>{choose}</button>
      )}
    </div>
  );
}

/**
 * An empty block a reader can't fill (a locked or read-only page): its row
 * as an editor sees it, in its place and pressed to no end, as Notion's keeps
 * it (seen 2026-11-08: a locked page's empty picture and PDF rows unchanged).
 */
export function BlockEmptyRow({ name, icon, label }: { name: string; icon: ReactNode; label: string }) {
  return (
    <div className={styles.root} contentEditable={false} data-block-empty={name} data-read-only="true">
      <div className={styles.row} aria-disabled="true" data-block-empty-row>
        <span className={styles.rowIcon} aria-hidden="true">{icon}</span>
        <span className={styles.rowLabel} data-block-empty-label>{label}</span>
      </div>
    </div>
  );
}

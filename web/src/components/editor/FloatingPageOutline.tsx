import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { followScreenKind, touchScreen } from "@/lib/screenKind";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { PageOutlineLink, usePageOutlineNavigation } from "./usePageOutlineNavigation";
import { watchPageSize } from "../page/pageSizeWatch";
import styles from "./FloatingPageOutline.module.css";

// The outline's marks take 56px from the scroll area's right edge; it needs
// a little air beside the text too.
const OUTLINE_ROOM = 64;
// Its marks end 20px inside the page's scroll area, beside its scrollbar,
// the first 130px under that area's top, in a page and in a center peek's
// window alike, as Notion's (seen 2026-11-14 at 1440: a page's marks end at
// 1405 beside the scrollbar from 1425, the first at 174 under the bar from
// 44; a center peek's end at 1172.5 inside its window's scrollbar from
// 1192.5, the first at 246 under its bar from 116). Its box, where the
// pointer opens its list, runs from 20px before the marks to the scrollbar
// and from the first mark to 24px under the last (the module's padding), as
// Notion's (seen 2026-11-14 and 2026-11-15 at 1440: 1369..1425 from 174, 68
// tall for four marks; the list opened at 1371 and 1423, not at 1366).
const FIRST_MARK_UNDER_TOP = 130;

// The box a fixed element is placed in, in that box's own pixels: the
// window, or the nearest ancestor that transforms or contains it — a center
// peek's window stands centered by a transform, and the outline stood over
// its text, 256px in from its edge, measured from the window's.
function fixedFrame(element: HTMLElement) {
  for (let box = element.parentElement; box; box = box.parentElement) {
    const style = getComputedStyle(box);
    if (style.transform === "none" && style.translate === "none" && style.scale === "none" && style.rotate === "none"
      && style.perspective === "none" && style.filter === "none" && style.backdropFilter === "none"
      && !/transform|translate|scale|rotate|perspective|filter/u.test(style.willChange)
      && !/paint|layout|strict|content/u.test(style.contain) && style.containerType === "normal") continue;
    const rect = box.getBoundingClientRect();
    // A frame scaled as it opens (a center peek grows from 98%) reads in its own pixels.
    const scale = box.offsetWidth > 0 ? rect.width / box.offsetWidth : 1;
    return { left: rect.left, top: rect.top, scale: scale || 1, inline: box.clientLeft, block: box.clientTop, width: box.clientWidth };
  }
  return { left: 0, top: 0, scale: 1, inline: 0, block: 0, width: document.documentElement.clientWidth || window.innerWidth };
}

export function FloatingPageOutline({ adapter }: { adapter:NativeEditorGraphAdapter }) {
  const {t} = useTranslation("rowMenu");
  const host = useRef<HTMLElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const skipFocusOpen = useRef(false);
  const [open,setOpen] = useState(false);
  const [position,setPosition] = useState({right:24,top:160,visible:false});
  const {headings,activeId,jump} = usePageOutlineNavigation(adapter,host);
  // Fewer than two headings show none: there is nothing to place. Where it
  // stands doesn't change with how many there are: a heading made or pasted
  // laid out the page it had just changed to place it again, inside the
  // press (2026-11-15: some 5 ms of a paste in a 1,031-block page at CPU ×2).
  const placed = headings.length >= 2;
  // It floats only in the room right of the text, as Notion's, which shows
  // none on a narrow screen (2026-10-16: at 390px it lay over the text's end)
  // nor in a side peek (2026-11-07: a 720 side peek's page with headings),
  // and only for a pointer: a touch screen shows none at any width, as
  // Notion's iPad (seen 2026-11-15 at 834 and 1210, where Notion's window in
  // Chrome with a mouse shows it at 1194 and 1440, seen 2026-11-16).
  useLayoutEffect(() => {
    const scope = host.current?.closest<HTMLElement>("[data-page-scroll]"); if (!scope) return;
    if (host.current?.closest('[data-row-page-mode="side"]')) { setPosition((current) => ({ ...current, visible: false })); return; }
    if (!placed) return;
    const measure = () => {
      const rect=scope.getBoundingClientRect();
      const text=scope.querySelector<HTMLElement>("[data-block-id]")?.getBoundingClientRect();
      // Until a block is drawn there is no text to measure the room by.
      const room=text ? rect.right-text.right : 0;
      const frame=host.current ? fixedFrame(host.current) : null;
      if (!frame) return;
      // The scroll area's inside edge (its scrollbar's) and top, in the frame's pixels.
      const end=(rect.left-frame.left)/frame.scale-frame.inline+scope.clientLeft+scope.clientWidth;
      const top=(rect.top-frame.top)/frame.scale-frame.block;
      const next={right:Math.round((frame.width-end)*10)/10,top:Math.round((top+FIRST_MARK_UNDER_TOP)*10)/10,
        visible:rect.width>0 && room>=OUTLINE_ROOM && !touchScreen()};
      setPosition((current) => current.right===next.right && current.top===next.top && current.visible===next.visible ? current : next);
    };
    // The blocks are often drawn after the first measure (a page opening,
    // changing or growing), which leaves the scroll area's size as it was, so
    // the text beside the outline is watched too (2026-11-01: a page measured
    // empty kept its outline over a phone's text). It measures with the
    // page's other size measures, before any of them writes (pageSizeWatch):
    // with an observer of its own it read the page after their writes and
    // laid it out again, 57 ms as a row's side peek opened beside a page of
    // three tables (seen 2026-11-13).
    measure();
    const parent=host.current?.parentElement;
    const stops=[watchPageSize(scope,measure),...(parent ? [watchPageSize(parent,measure)] : []),followScreenKind(measure)];
    window.addEventListener("resize",measure);
    return () => {for (const stop of stops) stop();window.removeEventListener("resize",measure);};
  }, [placed]);
  const visible = position.visible && placed;
  useEffect(() => { if (!visible) setOpen(false); }, [visible]);
  // A press beside its list closes it and goes on to what it pressed: the
  // list is there while the pointer or the focus rests on it and takes no
  // press from the page, as Notion's, whose list is gone as the pointer
  // leaves it. Its layer leaves a draw and an effect after the pointer does,
  // so a press that came sooner — the pointer straight from the marks to the
  // page's "⋯", in a browser driven by a script or one kept busy — only
  // closed it, and the "⋯" opened nothing (2026-11-18).
  // Escape is the page's while the list only shows under the pointer: the list stays and the key goes on — the text
  // holding the caret chooses its block, a page with no caret takes it nowhere —, as Notion's (2026-11-26, Chrome 1440,
  // judged by the screen after the key with nothing run in the page before it: the caret at a line's end in two tabs
  // at two times, no caret in two tabs at two times). With the focus in it, Escape gives it to the marks and closes it.
  const [focusIn,setFocusIn] = useState(false);
  useOverlayLayer(open && visible,{kind:"card",ref:host,escapeGoesOn:!focusIn,
    onEscape:focusIn ? ()=>{skipFocusOpen.current=true;trigger.current?.focus({preventScroll:true});setOpen(false);} : undefined,
    onOutsidePress:()=>setOpen(false)});
  return <nav ref={host} contentEditable={false} className={styles.outline} data-floating-page-outline
    hidden={!visible} style={{right:position.right,top:position.top}}
    aria-label={t("customize.tableOfContents")} onMouseEnter={()=>setOpen(true)} onMouseLeave={()=>{if (!host.current?.contains(document.activeElement)) setOpen(false);}}
    onFocus={()=>setFocusIn(true)} onBlur={event=>{if (!event.currentTarget.contains(event.relatedTarget)) {setFocusIn(false);setOpen(false);}}}>
    <button ref={trigger} type="button" className={styles.indicators} aria-label={t("customize.tableOfContents")}
      aria-expanded={open} onFocus={()=>{if (skipFocusOpen.current) skipFocusOpen.current=false;else setOpen(true);}} onClick={()=>setOpen(true)}>
      {headings.map(heading=><span key={heading.block.id} data-level={heading.level} data-active={activeId===heading.block.id || undefined} />)}
    </button>
    {open ? <div className={styles.panel}>
      {headings.map(heading=><PageOutlineLink key={heading.block.id} adapter={adapter} heading={heading}
        className={styles.link} active={activeId===heading.block.id} onJump={(id,event)=>{jump(id,event);}} />)}
    </div> : null}
  </nav>;
}

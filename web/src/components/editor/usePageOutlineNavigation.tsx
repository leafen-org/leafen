import { useEffect, useMemo, useRef, useState, type MouseEvent, type RefObject } from "react";
import { useCollaborativeText } from "@/collaboration";
import { motionSafeScrollBehavior } from "@/lib/motion";
import { blockAnchorId } from "./blockHandleModel";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { pageOutlineHeadings, type PageOutlineHeading } from "./pageOutlineModel";

export function usePageOutlineNavigation(adapter: NativeEditorGraphAdapter, host: RefObject<HTMLElement | null>) {
  const headings = useMemo(() => pageOutlineHeadings(adapter.graph.blocks), [adapter.graph.blocks]);
  const [activeId, setActiveId] = useState<string | null>(null);
  const flashTimer = useRef<number | undefined>(undefined);
  const target = (id: string) => {
    const scope = host.current?.closest("[data-page-scroll]") ?? document;
    return scope.querySelector<HTMLElement>(`#${CSS.escape(blockAnchorId(id))}`);
  };
  useEffect(() => {
    let frame = 0;
    const update = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        // A heading is the one read once its block's top has come up to
        // halfway between the top of the page's scroll area and the foot of
        // the window, as Notion's (seen 2026-11-14 at 1440×723: at 383.5 in
        // a page whose area starts at 44, at 419.5 in a center peek's whose
        // area starts at 116, every heading level alike).
        const scope = host.current?.closest("[data-page-scroll]");
        const line = ((scope?.getBoundingClientRect().top ?? 0) + window.innerHeight) / 2;
        let active = headings[0]?.block.id ?? null;
        for (const heading of headings) {
          const element = target(heading.block.id);
          if (!element) continue;
          if (element.getBoundingClientRect().top <= line) active = heading.block.id; else break;
        }
        setActiveId(active);
      });
    };
    update();
    window.addEventListener("scroll", update, {passive:true,capture:true}); window.addEventListener("resize",update);
    return () => { if (frame) window.cancelAnimationFrame(frame); window.removeEventListener("scroll",update,{capture:true}); window.removeEventListener("resize",update); };
  }, [headings]);
  useEffect(() => () => window.clearTimeout(flashTimer.current), []);
  const jump = (id: string, event: MouseEvent<HTMLAnchorElement>) => {
    event.preventDefault();
    const element = target(id); if (!element) return;
    if (window.location.hash !== `#${blockAnchorId(id)}`) window.history.pushState(null,"",`#${blockAnchorId(id)}`);
    element.scrollIntoView({behavior:motionSafeScrollBehavior(),block:"center",inline:"nearest"});
    element.dataset.commentFlash = "true"; window.clearTimeout(flashTimer.current);
    flashTimer.current = window.setTimeout(() => delete element.dataset.commentFlash,1400);
  };
  return {headings,activeId,jump};
}

export function PageOutlineLink({ adapter, heading, active, onJump, className }: {
  adapter:NativeEditorGraphAdapter; heading:PageOutlineHeading; active:boolean;
  onJump:(id:string,event:MouseEvent<HTMLAnchorElement>)=>void; className:string;
}) {
  const live = useCollaborativeText(adapter.collaborationDocument, heading.block.id);
  const text = adapter.collaborationDocument ? live : adapter.text(heading.block);
  return <a className={className} data-level={heading.level} data-active={active || undefined}
    aria-current={active ? "location" : undefined} href={`#${blockAnchorId(heading.block.id)}`}
    onClick={event => onJump(heading.block.id,event)}><span>{text.trim() || blockItemText("common.untitled")}</span></a>;
}

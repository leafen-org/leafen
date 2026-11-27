import { memo, useCallback, useEffect, useLayoutEffect, useState, useSyncExternalStore, type ReactPortal } from "react";
import { createPortal } from "react-dom";
import { PureEditorContent, type ReactRenderer } from "@tiptap/react";
import type { Editor } from "@tiptap/core";
import { NODE_VIEWS_DRAWN_EVENT } from "./nativeTiptapLiveState";

// Tiptap draws every React node view in one list: a change to one view —
// each typed letter — drew the list of all of them again, and a long page
// committed all 6,000 of its views for it, 15 ms a letter on a 3,000-block
// page (2026-10-18). Here each view is a part of its own, drawn again when
// it changes; the list is drawn again only when views come or go.
//
// The list itself is drawn in groups of views made one after another: a view
// coming or going drew the list again with each of a long page's 6,000 views
// in it, and React, finding each one unchanged, looked through every part
// above it to the page's root for a changed provider (8 ms of an Enter or a
// ⌘V at CPU ×2 on a 1,031-block page, 2026-11-14). A view coming or going
// now draws its own group again, and the list goes through the groups only.
//
// A long page opening drew all its views in one task, a second with the
// screen still. The views it opens with are drawn from the top, the first
// screenful at once and the rest a part a frame, as Notion fills a long
// page; the document holds every block meanwhile, and views made after
// (a typed block, a paste) are drawn at once.

const OPENING_AT_ONCE = 240;
const OPENING_PART = 480;
// A page opening makes more views at once than this.
const OPENING_FROM = 600;
// Views in a group of the list.
const GROUP_SIZE = 64;


type Slot = {
  portal: ReactPortal;
  listeners: Set<() => void>;
};

// A group of the list: the views it holds, kept as the same list while none
// of them comes or goes.
type Group = { key: number; members: Set<string>; ids: readonly string[] };
type ViewList = { ids: readonly string[]; groups: readonly Group[] };

function slotContent(existing: Record<string, ReactPortal>) {
  const slots = new Map<string, Slot>();
  const groups: Group[] = [];
  const groupOf = new Map<string, Group>();
  const touched = new Set<Group>();
  let nextGroup = 0;
  // A new view joins the last group made, or a new one when that is full.
  const join = (id: string) => {
    let group = groups.at(-1);
    if (!group || group.members.size >= GROUP_SIZE) {
      group = { key: nextGroup++, members: new Set(), ids: [] };
      groups.push(group);
    }
    group.members.add(id);
    groupOf.set(id, group);
    touched.add(group);
  };
  const leave = (id: string) => {
    const group = groupOf.get(id);
    if (!group) return;
    groupOf.delete(id);
    group.members.delete(id);
    touched.add(group);
  };
  const viewList = (): ViewList => {
    for (const group of touched) group.ids = [...group.members];
    touched.clear();
    for (let at = groups.length - 1; at >= 0; at -= 1) if (!groups[at].members.size) groups.splice(at, 1);
    return { ids: [...slots.keys()], groups: [...groups] };
  };
  for (const [id, portal] of Object.entries(existing)) {
    slots.set(id, { portal, listeners: new Set() });
    join(id);
  }
  const listListeners = new Set<() => void>();
  let list = viewList();
  let record: Record<string, ReactPortal> | null = null;
  let listQueued = false;
  const changed = new Set<Slot>();
  let slotsQueued = false;
  // Changes are told after the editor's own work, as Tiptap tells them.
  const listChanged = () => {
    record = null;
    if (listQueued) return;
    listQueued = true;
    queueMicrotask(() => {
      listQueued = false;
      list = viewList();
      for (const listener of [...listListeners]) listener();
    });
  };
  const slotChanged = (slot: Slot) => {
    changed.add(slot);
    if (slotsQueued) return;
    slotsQueued = true;
    queueMicrotask(() => {
      slotsQueued = false;
      const now = [...changed];
      changed.clear();
      for (const each of now) for (const listener of [...each.listeners]) listener();
    });
  };
  return {
    slot: (id: string) => slots.get(id),
    list: () => list,
    subscribeList(listener: () => void) {
      listListeners.add(listener);
      return () => { listListeners.delete(listener); };
    },
    setRenderer(id: string, renderer: ReactRenderer) {
      const portal = createPortal(renderer.reactElement, renderer.element, id);
      const slot = slots.get(id);
      if (!slot) {
        slots.set(id, { portal, listeners: new Set() });
        join(id);
        listChanged();
        return;
      }
      slot.portal = portal;
      slotChanged(slot);
    },
    removeRenderer(id: string) {
      if (!slots.delete(id)) return;
      leave(id);
      listChanged();
    },
    // What Tiptap's own list reads, for anything else that asks.
    subscribe(listener: () => void) {
      listListeners.add(listener);
      return () => { listListeners.delete(listener); };
    },
    getSnapshot() {
      record ??= Object.fromEntries([...slots].map(([id, slot]) => [id, slot.portal]));
      return record;
    },
    getServerSnapshot() {
      record ??= Object.fromEntries([...slots].map(([id, slot]) => [id, slot.portal]));
      return record;
    },
  };
}

type SlotContent = ReturnType<typeof slotContent>;

// The page around the editor drawing again draws neither the list nor its views.
const NodeViewSlot = memo(function NodeViewSlot({ content, id }: { content: SlotContent; id: string }) {
  const slot = content.slot(id);
  const subscribe = useCallback((listener: () => void) => {
    if (!slot) return () => {};
    slot.listeners.add(listener);
    return () => { slot.listeners.delete(listener); };
  }, [slot]);
  return useSyncExternalStore(subscribe, () => slot?.portal ?? null);
});

// A group of the list draws again only when a view of it comes or goes, or
// is shown at the page's opening.
const NodeViewGroup = memo(function NodeViewGroup({ content, ids, hidden }: {
  content: SlotContent; ids: readonly string[]; hidden: ReadonlySet<string> | null;
}) {
  return <>{ids.map((id) => hidden?.has(id) ? null : <NodeViewSlot key={id} content={content} id={id} />)}</>;
});

const NodeViewSlots = memo(function NodeViewSlots({ content }: { content: SlotContent }) {
  const { ids, groups } = useSyncExternalStore(content.subscribeList, content.list);
  // The views still to draw of the page's opening, top first.
  const [opening, setOpening] = useState<{ waiting: readonly string[]; seen: boolean }>({ waiting: [], seen: false });
  if (!opening.seen && ids.length > 0) {
    setOpening({ waiting: ids.length > OPENING_FROM ? ids.slice(OPENING_AT_ONCE) : [], seen: true });
  }
  const waiting = opening.waiting;
  useEffect(() => {
    if (!opening.seen) return;
    if (!waiting.length) return;
    const frame = window.requestAnimationFrame(() => {
      const rest = waiting.slice(OPENING_PART);
      setOpening({ waiting: rest, seen: true });
      if (!rest.length) window.dispatchEvent(new Event(NODE_VIEWS_DRAWN_EVENT));
    });
    return () => window.cancelAnimationFrame(frame);
  }, [opening.seen, waiting]);
  useLayoutEffect(() => {
    if (opening.seen && !waiting.length) openedContents.add(content);
  }, [content, opening.seen, waiting]);
  const hidden = waiting.length ? new Set(waiting) : null;
  return <>{groups.map((group) => <NodeViewGroup key={group.key} content={content} ids={group.ids}
    hidden={hidden && group.ids.some((id) => hidden.has(id)) ? hidden : null} />)}</>;
});

const slotContents = new WeakSet<object>();
// Contents that have drawn all the views their page opened with.
const openedContents = new WeakSet<object>();

/** Whether an editor has drawn all the views its page opened with: a view made since is new to the page. */
export function nodeViewsOpened(editor: Editor) {
  const content = (editor as WithContent).contentComponent;
  return Boolean(content && openedContents.has(content));
}

// Editors whose owner is leaving with their content (Editor.tsx), told before
// the content unmounts and taken back if the owner stays after all.
const editorsGoing = new WeakSet<object>();
export function editorGoing(editor: Editor, going: boolean) {
  if (going) editorsGoing.add(editor); else editorsGoing.delete(editor);
}

// Tiptap keeps the content on the editor without naming it in its type.
type WithContent = Editor & { contentComponent?: { getSnapshot(): Record<string, ReactPortal> } | null };

/** Tiptap's editor content with node views drawn each on their own. */
export class NativeEditorContent extends PureEditorContent {
  override init() {
    const editor = this.props.editor as WithContent | null;
    super.init();
    const current = editor?.contentComponent;
    if (!editor || !current || slotContents.has(current)) return;
    // Views made while Tiptap set itself up move over as they are.
    const content = slotContent(current.getSnapshot());
    slotContents.add(content);
    editor.contentComponent = content;
    this.forceUpdate();
  }

  // Tiptap's unmount keeps the editor for another content: it draws the
  // whole document again without its React views, taking each view's
  // elements out of the page, and moves what is left out of the page. An
  // editor its owner destroys right after (the page leaving) empties itself
  // then, out of the page: a long page's leaving did that work for nothing
  // in the frame it left in (2026-11-13: two fifths of the teardown of a
  // 37,000-element page, as profiled).
  override componentWillUnmount() {
    const editor = this.props.editor;
    if (editor && editorsGoing.has(editor) && !editor.isDestroyed) return;
    super.componentWillUnmount();
  }

  override render() {
    const { editor, innerRef, ...rest } = this.props;
    const content = (editor as WithContent | null)?.contentComponent;
    return <>
      <div {...rest} ref={(node) => {
        (this.editorContentRef as { current: unknown }).current = node;
        if (typeof innerRef === "function") innerRef(node);
        else if (innerRef) innerRef.current = node;
      }} />
      {content && slotContents.has(content) ? <NodeViewSlots content={content as SlotContent} /> : null}
    </>;
  }
}

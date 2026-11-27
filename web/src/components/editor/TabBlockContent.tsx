import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import type { ContentGraph, GraphBlock } from "@/contentGraph";
import { useTypedDraftSave } from "@/lib/useTypedDraftSave";
import { useDragReorder } from "@/lib/useDragReorder";
import { Plus } from "@/icons/leafen";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { PageIconPicker } from "@/components/page/PageIconPicker";
import { copyText } from "@/lib/copyText";
import { absolutePageUrl } from "@/lib/navigation";
import { useOptionalProductToast } from "@/lib/productToast";
import type { IconType } from "@/lib/types";
import { blockAnchorId } from "./blockHandleModel";
import { TabBlockMenu } from "./TabBlockMenu";
import { blockLinkTarget } from "@/lib/blockAddress";
import { revealLinkedBlock } from "./useBlockAnchorScroll";
import { focusMadeLine } from "./focus";
import { blockItemText } from "./blockItemLabels";
import { tabItemName } from "./tabItemName";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";
import { fieldEnter, imeComposing } from "@/lib/fieldKeyBoundary";
import { keysFocusHolder } from "@/lib/lastPress";

// A tab row the keys' focus is in shows its "+" (data-keys-within).
keysFocusHolder(styles.tabList);

function tabButtonId(blockId: string, tabId: string) {
  return `tab-${blockId}-${tabId}`;
}

function tabPanelId(blockId: string, tabId: string) {
  return `tabpanel-${blockId}-${tabId}`;
}

// The tab of a tab group that a block is, or sits inside of.
function tabHolding(graph: ContentGraph, groupId: string, blockId: string) {
  const byId = new Map(graph.blocks.map((block) => [block.id, block]));
  for (let current = byId.get(blockId); current; current = current.parentId ? byId.get(current.parentId) : undefined) {
    if (current.parentId === groupId) return current.id;
  }
  return null;
}

// A tab's icon, stored as a callout's: an emoji, a built-in icon, an image or a custom emoji.
function tabIcon(tab: GraphBlock): { icon: string; iconType: IconType } | null {
  const content = tab.content;
  if (content.iconType === "custom_emoji" && typeof content.customEmojiId === "string") return { icon: content.customEmojiId, iconType: "custom_emoji" };
  if (typeof content.icon !== "string" || !content.icon) return null;
  if (content.iconType === "external") return { icon: content.icon, iconType: "image" };
  if (content.iconType === "icon") return { icon: content.icon, iconType: "icon" };
  return { icon: content.icon, iconType: "emoji" };
}

export function TabBlockContent({
  adapter,
  block,
  items,
  panels,
  renderPanel = true,
  onActiveTabChange,
  onRename,
}: {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  items: GraphBlock[];
  panels: ReadonlyMap<string, ReactNode>;
  renderPanel?: boolean;
  onActiveTabChange?(id: string): void;
  /** A tab's name opened to type over; the block is no longer selected, as Notion's. */
  onRename?(): void;
}) {
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [editingTabId, setEditingTabId] = useState<string | null>(null);
  const [editingLabel, setEditingLabel] = useState("");
  const [menu, setMenu] = useState<{ itemId: string; opener: HTMLElement } | null>(null);
  const [iconFor, setIconFor] = useState<string | null>(null);
  const toast = useOptionalProductToast();
  const [, setTextRevision] = useState(0);
  const activeTab = items.find((item) => item.id === activeTabId) ?? items[0];
  useEffect(() => {
    if (activeTab) onActiveTabChange?.(activeTab.id);
  }, [activeTab?.id, onActiveTabChange]);
  // A tab's new name saves while it is typed, as a page title does.
  const renameBeganWith = useRef("");
  const typedRename = useTypedDraftSave({
    active: editingTabId !== null && !adapter.readOnly, draft: editingLabel.trim(), initial: renameBeganWith.current,
    save: (label) => {
      const item = items.find((candidate) => candidate.id === editingTabId);
      if (item && label) adapter.changeText(item, label);
    },
  });

  useEffect(() => {
    const document = adapter.collaborationDocument;
    if (!document) return;
    const onUpdate = () => setTextRevision((revision) => revision + 1);
    document.on("update", onUpdate);
    return () => document.off("update", onUpdate);
  }, [adapter.collaborationDocument]);

  useEffect(() => {
    if (items.length === 0) {
      if (activeTabId) setActiveTabId(null);
      return;
    }
    if (!activeTab || activeTab.id !== activeTabId) setActiveTabId(activeTab.id);
  }, [activeTab, activeTabId, items]);

  // A block link to a tab, or to a block inside one — the link the tab's
  // menu copies — opens that tab and shows it. It follows the first tab's
  // default above, so a page opened at the link opens at that tab.
  const followedLink = useRef("");
  const itemKey = items.map((item) => item.id).join(" ");
  useEffect(() => {
    function follow() {
      const linked = blockLinkTarget(window.location.hash);
      if (!linked) followedLink.current = "";
      const itemId = linked ? tabHolding(adapter.graph, block.id, linked) : null;
      if (!linked || !itemId || followedLink.current === linked) return;
      followedLink.current = linked;
      setEditingTabId(null);
      setActiveTabId(itemId);
      // The tab's content shows once the open tab has rendered.
      window.requestAnimationFrame(() => window.requestAnimationFrame(() => {
        const target = document.getElementById(blockAnchorId(linked === itemId ? block.id : linked));
        if (target) revealLinkedBlock(target);
      }));
    }
    follow();
    window.addEventListener("hashchange", follow);
    return () => window.removeEventListener("hashchange", follow);
  }, [adapter, block.id, itemKey]);

  function labelFor(item: GraphBlock) {
    return tabItemName(adapter, item).trim() || blockItemText("tabs.untitled");
  }

  function focusTab(itemId: string) {
    window.requestAnimationFrame(() => document.getElementById(tabButtonId(block.id, itemId))?.focus());
  }

  function activateTab(index: number, focus = false) {
    const next = items[index];
    if (!next) return;
    setEditingTabId(null);
    setActiveTabId(next.id);
    if (focus) focusTab(next.id);
  }

  function moveTab(index: number, direction: -1 | 1) {
    if (adapter.readOnly) return;
    const item = items[index];
    const target = items[index + direction];
    if (!item || !target) return;
    const afterId = direction < 0 ? items[index - 2]?.id ?? null : target.id;
    adapter.move(item.id, block.id, afterId);
    setEditingTabId(null);
    setActiveTabId(item.id);
    focusTab(item.id);
  }

  function onTabKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    if (items.length === 0) return;
    if (
      (event.altKey || event.metaKey) &&
      ["ArrowLeft", "ArrowUp", "ArrowRight", "ArrowDown"].includes(event.key)
    ) {
      event.preventDefault();
      event.stopPropagation();
      moveTab(index, event.key === "ArrowLeft" || event.key === "ArrowUp" ? -1 : 1);
      return;
    }
    let nextIndex: number | null = null;
    if (event.key === "ArrowRight" || event.key === "ArrowDown") nextIndex = (index + 1) % items.length;
    if (event.key === "ArrowLeft" || event.key === "ArrowUp") nextIndex = (index - 1 + items.length) % items.length;
    if (event.key === "Home") nextIndex = 0;
    if (event.key === "End") nextIndex = items.length - 1;
    if (nextIndex === null) return;
    event.preventDefault();
    event.stopPropagation();
    activateTab(nextIndex, true);
  }

  // A tab drags along its own group only; it lands on the side of the tab
  // it is dropped on and stays the open tab.
  const tabReorder = useDragReorder(`tab-block:${block.id}`, (sourceId, targetId, side) => {
    const sourceIndex = items.findIndex((item) => item.id === sourceId);
    const remaining = items.filter((item) => item.id !== sourceId);
    const targetIndex = remaining.findIndex((item) => item.id === targetId);
    const insertionIndex = targetIndex + (side === "after" ? 1 : 0);
    if (sourceIndex < 0 || targetIndex < 0 || insertionIndex === sourceIndex) return;
    adapter.move(sourceId, block.id, remaining[insertionIndex - 1]?.id ?? null);
    setEditingTabId(null);
    setActiveTabId(sourceId);
    focusTab(sourceId);
  }, { axis: "horizontal", onDragStart: setActiveTabId });

  // As Notion's "+": the next "Tab N", open and empty, its name chosen to type over.
  function addTab() {
    if (adapter.readOnly) return;
    const label = blockItemText("tabs.newNamed", { number: items.length + 1 });
    const created = adapter.insertTabItem(block.id, label);
    if (!created) return;
    setActiveTabId(created.item.id);
    renameBeganWith.current = label;
    setEditingLabel(label);
    setEditingTabId(created.item.id);
    onRename?.();
  }

  function beginRename(item: GraphBlock) {
    if (adapter.readOnly) return;
    setActiveTabId(item.id);
    renameBeganWith.current = labelFor(item);
    setEditingLabel(labelFor(item));
    setEditingTabId(item.id);
    onRename?.();
  }

  function commitRename(item: GraphBlock) {
    if (adapter.readOnly) return;
    typedRename.settle();
    const label = editingLabel.trim() || blockItemText("common.untitled");
    adapter.changeText(item, label);
    setEditingTabId(null);
    focusTab(item.id);
  }

  function cancelRename(item: GraphBlock) {
    typedRename.settle();
    const restore = typedRename.restoreValue();
    if (restore !== null) adapter.changeText(item, restore);
    setEditingTabId(null);
    focusTab(item.id);
  }

  function removeTab(item: GraphBlock, index: number) {
    if (adapter.readOnly || items.length <= 1) return;
    const next = items[index - 1] ?? items[index + 1];
    setEditingTabId(null);
    setActiveTabId(next?.id ?? null);
    adapter.remove([item.id]);
    if (next) focusTab(next.id);
  }

  function openMenu(item: GraphBlock, opener: HTMLElement) {
    setMenu({ itemId: item.id, opener });
  }

  // A link that opens the page at the tab, as a block's link does.
  async function copyTabLink(item: GraphBlock) {
    const url = new URL(absolutePageUrl(adapter.pageId));
    url.hash = blockAnchorId(item.id);
    const ok = await copyText(url.toString());
    toast?.notify(blockItemText(ok ? "tabs.menu.linkCopied" : "tabs.menu.linkNotCopied"), ok ? "success" : "error");
  }

  function pickIcon(item: GraphBlock, change: Record<string, unknown>) {
    adapter.updateBlock(item, { customEmojiId: "", ...change });
    setIconFor(null);
  }

  if (items.length === 0) {
    return <div className={styles.tabBlock}>
      <button type="button" contentEditable={false} className={styles.toggleEmptyChild} disabled={adapter.readOnly}
        aria-label={blockItemText("tabs.add")} title={blockItemText("tabs.add")} onClick={addTab}>
        {blockItemText("tabs.emptyGroup")}
      </button>
    </div>;
  }

  const panelId = activeTab ? tabPanelId(block.id, activeTab.id) : undefined;
  return <div className={styles.tabBlock}>
    <div className={styles.tabList} role="tablist" aria-label={blockItemText("tabs.label")}>
      {items.map((item, index) => {
        const label = labelFor(item);
        const active = item.id === activeTab?.id;
        const editing = item.id === editingTabId;
        const icon = tabIcon(item);
        const { "data-drop-side": dropSide, ...reorder } = tabReorder.row(item.id, () => true, !adapter.readOnly && !editing);
        return <span key={item.id} className={styles.tabButtonWrap} {...reorder} data-tab-drop={dropSide}>
          {editing ? <input className={styles.tabLabelInput} aria-label={blockItemText("tabs.rename", { label })}
            size={Math.max(1, [...editingLabel].length)} value={editingLabel} autoFocus onFocus={(event) => event.currentTarget.select()}
            onChange={(event) => setEditingLabel(event.currentTarget.value)}
            onBlur={() => commitRename(item)} onKeyDown={(event) => {
              if (imeComposing(event)) return;
              if (fieldEnter(event)) {
                event.preventDefault();
                commitRename(item);
              } else if (event.key === "Escape") {
                event.preventDefault();
                cancelRename(item);
              }
            }} /> : <button id={tabButtonId(block.id, item.id)} type="button" className={styles.tabButton}
              data-active={active ? "true" : undefined} role="tab" aria-label={label} aria-selected={active}
              aria-controls={active ? panelId : undefined} tabIndex={active ? 0 : -1}
              onClick={() => activateTab(index)} onDoubleClick={() => beginRename(item)}
              onContextMenu={(event) => {
                event.preventDefault();
                openMenu(item, event.currentTarget);
              }}
              onKeyDown={(event) => {
                if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10")) {
                  event.preventDefault();
                  openMenu(item, event.currentTarget);
                  return;
                }
                if (fieldEnter(event) && active && !adapter.readOnly) {
                  event.preventDefault();
                  event.stopPropagation();
                  beginRename(item);
                  return;
                }
                onTabKeyDown(event, index);
              }}>
              {icon ? <span className={styles.tabIcon}>
                <PageIconGlyph page={{ ...icon, workspaceId: adapter.workspaceId ?? undefined }} size={15} />
              </span> : null}<span>{label}</span>
            </button>}
          {iconFor === item.id && !adapter.readOnly ? (
            <PageIconPicker
              placement="inline" current={icon ?? undefined}
              onPick={(emoji) => pickIcon(item, { icon: emoji, iconType: "emoji" })}
              onPickImage={(url) => pickIcon(item, { icon: url, iconType: "external" })}
              onPickBuiltInIcon={(value) => pickIcon(item, { icon: value, iconType: "icon" })}
              onPickCustomEmoji={(emoji) => pickIcon(item, { icon: emoji.url, iconType: "custom_emoji", customEmojiId: emoji.id })}
              onRemove={icon ? () => pickIcon(item, { icon: "", iconType: "" }) : undefined}
              onClose={() => setIconFor(null)}
            />
          ) : null}
        </span>;
      })}
      {menu ? (() => {
        const index = items.findIndex((candidate) => candidate.id === menu.itemId);
        const item = items[index];
        if (!item) return null;
        const writable = !adapter.readOnly;
        return <TabBlockMenu opener={menu.opener} label={labelFor(item)} deletable={writable && items.length > 1}
          writable={writable}
          onRename={() => beginRename(item)} onEditIcon={() => setIconFor(item.id)}
          onCopyLink={() => void copyTabLink(item)} onDelete={() => removeTab(item, index)}
          onClose={() => setMenu(null)} />;
      })() : null}
      {!adapter.readOnly ? <button type="button" className={styles.tabAddButton}
        aria-label={blockItemText("tabs.add")} title={blockItemText("tabs.add")} onClick={addTab}>
        <Plus size="regular" aria-hidden="true" />
      </button> : null}
    </div>
    {activeTab && renderPanel ? <div id={panelId} className={styles.tabPanel} role="tabpanel"
      aria-labelledby={tabButtonId(block.id, activeTab.id)}>
      {panels.get(activeTab.id) ?? <button type="button" contentEditable={false} className={styles.toggleEmptyChild}
        disabled={adapter.readOnly} aria-label={blockItemText("tabs.addBlockInsideEmpty")}
        title={blockItemText("tabs.addBlockInside")} onClick={() => focusMadeLine(() => adapter.insertChild(activeTab.id), activeTab.id)}>{blockItemText("tabs.empty")}</button>}
    </div> : null}
  </div>;
}

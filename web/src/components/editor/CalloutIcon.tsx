import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { GraphBlock } from "@/contentGraph";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { AnchoredIconPicker, PICKER_WIDTH, usePageIconPickerAnchor } from "@/components/page/AnchoredEmojiPicker";
import { POPOVER_MARGIN } from "@/lib/popoverPlacement";
import { takeIconCompanions } from "./blockIconCompanions";
import { calloutGlyphSize, calloutPictureIcon, storedBlockIcon } from "./blockIconModel";
import { blockItemText } from "./blockItemLabels";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import styles from "./editorStyles.generated.module.css";

const EDIT_CALLOUT_ICON = "leafen:edit-callout-icon";

/** A callout's picker suggests these in its "콜아웃" row, Notion's, in its order (seen 2026-11-06). */
const CALLOUT_EMOJIS = ["💡", "👉", "☝️", "👌", "🔑", "🚧", "⚠️", "🔥", "📌", "✂️", "❓", "🚫", "⛔", "⏰", "☎️",
  "🚨", "♻️", "✅", "🔒", "📎", "📖", "🗣️", "➡️", "📢", "🛠️", "⚙️"] as const;

/** Opens a callout's icon picker, as its menu's "아이콘 편집" does (seen 2026-10-11). */
export function requestCalloutIconEdit(blockId: string) {
  window.dispatchEvent(new CustomEvent<string>(EDIT_CALLOUT_ICON, { detail: blockId }));
}

/** The callout's icon button, which opens the page icon picker as Notion's does. */
export function CalloutIcon({ adapter, block }: { adapter: NativeEditorGraphAdapter; block: GraphBlock }) {
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLSpanElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  // Several chosen callouts' "아이콘 편집" opens this one's picker for them all (blockIconCompanions).
  const companions = useRef<readonly string[]>([]);
  useEffect(() => {
    const listener = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== block.id || adapter.readOnly) return;
      companions.current = takeIconCompanions(block.id);
      setOpen(true);
    };
    window.addEventListener(EDIT_CALLOUT_ICON, listener);
    return () => window.removeEventListener(EDIT_CALLOUT_ICON, listener);
  }, [adapter.readOnly, block.id]);
  // Taken away, it has none and its lines start where it stood; "아이콘 편집"
  // in its menu brings the picker back (Notion's, seen 2026-11-05).
  const none = block.content.iconType === "none";
  // The picker opens centered under the icon, over the page's column as a
  // page's icon picker; with no icon, centered under the block's handle,
  // with emoji and icons only (Notion's, seen 2026-11-06).
  const iconAnchor = usePageIconPickerAnchor(open && !none, buttonRef);
  const [handleAnchor, setHandleAnchor] = useState<ReturnType<typeof underHandle>>(null);
  useLayoutEffect(() => { setHandleAnchor(open && none ? underHandle(wrapRef.current) : null); }, [open, none]);
  const anchor = none ? handleAnchor : iconAnchor;
  const icon = storedBlockIcon(block);
  const pick = (change: Record<string, unknown>) => {
    const others = companions.current.flatMap((id) => adapter.graph.blocks.find((one) => one.id === id && one.type === "callout") ?? []);
    for (const one of [block, ...others]) adapter.updateBlock(one, { customEmojiId: "", ...change });
    setOpen(false);
  };
  if (none && !open) return null;
  return (
    <span ref={wrapRef} className={styles.calloutIconWrap} contentEditable={false}>
      {none ? null : <button
        ref={buttonRef}
        type="button"
        className={styles.calloutIcon}
        data-emoji={!icon || icon.iconType === "emoji" ? "true" : undefined}
        aria-label={blockItemText("callout.changeIcon")}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={blockItemText("callout.changeIcon")}
        disabled={adapter.readOnly}
        onClick={() => { companions.current = []; setOpen(true); }}
      >
        {/* A linked picture that can't be shown leaves its place blank, as Notion's
            does for one Markdown named by a character alone (seen 2026-11-11). */}
        {icon ? <PageIconGlyph page={{ ...icon, workspaceId: adapter.workspaceId ?? undefined }} {...calloutGlyphSize(icon.iconType)}
          fallback={icon.iconType === "image" ? "none" : "💡"} /> : <span data-emoji-glyph="">💡</span>}
      </button>}
      {open && !adapter.readOnly && anchor ? (
        <AnchoredIconPicker anchor={anchor} marker="callout-icon-picker" rise={!none} onClose={() => setOpen(false)} picker={{
          suggestedEmojis: CALLOUT_EMOJIS,
          short: none, current: icon ?? undefined,
          // The workspace's emoji, picked or added, and a picture uploaded or linked as the icon,
          // as Notion's callout picker offers them: an emoji keeps its own file, an uploaded
          // picture is the callout's, kept as a file block keeps its file (seen 2026-11-07).
          uploadTarget: adapter.workspaceId ? { workspaceId: adapter.workspaceId, pageId: adapter.pageId } : undefined,
          onPick: (emoji) => pick({ icon: emoji, iconType: "emoji" }),
          onPickImage: none ? undefined : (value) => pick(calloutPictureIcon(value)),
          onPickBuiltInIcon: (value) => pick({ icon: value, iconType: "icon" }),
          onPickCustomEmoji: (emoji) => pick({ icon: emoji.url, iconType: "custom_emoji", customEmojiId: emoji.id }),
          onRemove: none ? undefined : () => pick({ icon: "", iconType: "none" }),
        }} />
      ) : null}
    </span>
  );
}

// Under the block's handle, 25px left of its column and 22px under its top:
// the picker's middle there and its top 16px under the handle's middle,
// shortened rather than raised where the room ends (Notion's, seen 2026-11-06).
function underHandle(wrap: HTMLElement | null) {
  const group = wrap?.closest<HTMLElement>("[data-block-id]");
  if (!group) return null;
  const width = () => Math.min(PICKER_WIDTH, window.innerWidth - 2 * POPOVER_MARGIN);
  return {
    get top() { return group.getBoundingClientRect().top; },
    get bottom() { return group.getBoundingClientRect().top + 34; },
    get left() { return group.getBoundingClientRect().left - 25 - width() / 2; },
    contextElement: group,
  };
}

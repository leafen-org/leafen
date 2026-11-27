import type { GraphBlock } from "@/contentGraph";
import { PageIconGlyph } from "@/components/page/PageIcon";
import { IconChoiceButton } from "@/components/page/IconChoiceButton";
import { blockItemText } from "./blockItemLabels";
import { storedBlockIcon } from "./blockIconModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";

export function PageButtonIcon({ adapter, block, picker = false }: {
  adapter: NativeEditorGraphAdapter; block: GraphBlock; picker?: boolean;
}) {
  const icon = storedBlockIcon(block);
  if (!picker) return icon ? <PageIconGlyph page={{ ...icon, workspaceId: adapter.workspaceId }} size={16} fallback="" /> : null;
  function pick(change: Record<string, unknown>) {
    if (!adapter.isCurrent() || adapter.readOnly) return;
    const live = adapter.graph.blocks.find(candidate => candidate.id === block.id);
    if (live) adapter.updateBlock(live, { customEmojiId: "", ...change });
  }
  // Its head's icon is Notion's "아이콘 추가" button, as a "페이지 아이콘 설정" step's (IconChoiceButton): an emoji or a
  // built-in icon, no upload (seen 2026-11-21); a picture or workspace emoji kept from before still shows.
  return <IconChoiceButton icon={icon} label={blockItemText("button.icon")} workspaceId={adapter.workspaceId} disabled={adapter.readOnly}
    marker="page-button-icon-picker"
    onChoose={next => pick(next ? { icon: next.icon, iconType: next.iconType } : { icon: "", iconType: "none" })} />;
}

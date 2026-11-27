import type { Session } from "@/types";
import type { BlockPickerSnapshot, MentionItem } from "./blockPickerModel";
import type { BlockDef, InlineSlashKind } from "./blocks";
import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { InlineEmojiMenu } from "./InlineEmojiMenu";
import { MentionMenu, type MentionPick } from "./MentionMenu";
import { showTodoMenuPreview, todoMenuPreviewOf, type TodoMenuTask } from "./todoTask";
import { SlashMenu, type SlashMenuAnchor, type SlashPageIn } from "./SlashMenu";

const slashBounds = {
  // Whatever is typed after "/" is searched (no length the menu stops at).
  search: { maxQueryCharacters: Number.MAX_SAFE_INTEGER, maxQueryTokens: Number.MAX_SAFE_INTEGER },
};

export type TextInlineMenu = {
  kind: "slash" | "mention" | "page-link" | "emoji" | "pasted-url";
  query: string;
  anchor?: SlashMenuAnchor;
  trigger?: { start: number; end: number };
  /** A mention the slash menu began keeps to its kind. */
  only?: "person" | "page" | "date";
  /** "+"'s page links lead with making the page. */
  newPageFirst?: boolean;
  /** An explicit insert menu has no line action or typed trigger. */
  lineCommands?: boolean;
  /** A typed "/" on a line holding nothing else (SlashMenu's "추천"). */
  lineEmpty?: boolean;
};

export function TextBlockMenus({
  blockId,
  pageId,
  menu,
  mentionSnapshot,
  newPages = false,
  invite = false,
  session,
  onClose,
  onPickMention,
  onPickEmoji,
  customEmoji,
  onPickBlock,
  onInline,
  pageIn, currentType, syncedBlocks, task, suggesting, workspaceId,
}: {
  blockId: string;
  pageId: string;
  /** The page's workspace, whose guests' "/" has no HTML lead (slashHtmlLead). */
  workspaceId?: string;
  menu: TextInlineMenu | null;
  mentionSnapshot: BlockPickerSnapshot | null;
  /** The "@" menu offers a new page named by the query. */
  newPages?: boolean;
  /** The "@" menu offers to invite the name typed to the page. */
  invite?: boolean;
  /** Who asks, so the "@" menu marks people the page doesn't open to. */
  session?: Session | null;
  onClose: () => void;
  onPickMention: (item: MentionItem, how?: MentionPick) => void;
  /** An emoji the ":" menu picked takes the ":" text's place. */
  onPickEmoji?: (emoji: string) => void;
  /** The workspace whose emoji the ":" menu also offers, and picking one. */
  customEmoji?: { workspaceId: string; onPick(emoji: WorkspaceCustomEmoji): void; onHold?(held: boolean): void };
  /** A block, "Turn into", an action or a color for the line. */
  onPickBlock: (definition: BlockDef) => void;
  /** An inline item goes into the line where "/" was typed. */
  onInline?: (kind: InlineSlashKind) => void;
  /** "페이지 위치:" of "Turn into": the pages to put the line's block in. */
  pageIn?: SlashPageIn;
  /** The line's kind, which its "Turn into" row ticks. */
  currentType?: string;
  /** Whether "/" offers a synced block, which it doesn't inside one (SlashMenu). */
  syncedBlocks?: boolean;
  /** The line is a to-do whose "@" assigns and sets its due date (MentionMenu). */
  task?: TodoMenuTask;
  /** Suggesting mode: "/" lists what it suggests there (suggestedSlashRows.ts). */
  suggesting?: boolean;
}) {
  // A pasted link's menu belongs to the text block.
  if (!menu || menu.kind === "pasted-url") return null;
  if (menu.kind === "slash") {
    return (
      <SlashMenu
        anchor={menu.anchor}
        bounds={slashBounds}
        ownerBlockId={blockId}
        query={menu.query}
        lineCommands={menu.lineCommands} lineEmpty={menu.lineEmpty}
        suggesting={suggesting}
        pageIn={pageIn}
        currentType={currentType}
        syncedBlocks={syncedBlocks} workspaceId={workspaceId}
        onClose={onClose}
        onPick={(definition: BlockDef) => {
          if (definition.inline) {
            if (onInline) onInline(definition.inline);
            else onClose();
            return;
          }
          onPickBlock(definition);
          onClose();
        }}
      />
    );
  }
  if (menu.kind === "emoji") {
    return onPickEmoji ? <InlineEmojiMenu anchor={menu.anchor} query={menu.query} onPick={onPickEmoji} onClose={onClose}
      custom={customEmoji ? { workspaceId: customEmoji.workspaceId, pageId, onPick: customEmoji.onPick, onHold: customEmoji.onHold } : undefined} /> : null;
  }
  if (!mentionSnapshot) return null;
  return (
    <MentionMenu
      ownerBlockId={blockId}
      anchor={menu.anchor}
      pageId={pageId}
      query={menu.query}
      mode={menu.kind === "page-link" ? "page_link" : "mention"}
      only={menu.only}
      newPageFirst={menu.newPageFirst}
      newPages={newPages}
      invite={invite}
      session={session}
      snapshot={mentionSnapshot}
      task={menu.kind === "mention" ? task : undefined}
      onActive={task && menu.kind === "mention" ? (item) => showTodoMenuPreview(todoMenuPreviewOf(blockId, item)) : undefined}
      onPick={onPickMention}
      onClose={onClose}
      docked
    />
  );
}

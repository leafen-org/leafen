import type { GraphBlock } from "@/contentGraph";
import type { TextSpan } from "@/lib/types";
import type { MentionItem } from "./blockPickerModel";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { personIsInvitee } from "@/lib/peopleDirectory";
import { mentionInsertion } from "./mentionInsertion";
import { pageUnreachablePeople } from "./mentionInvite";
import { todoAssignees, todoAssigneesChange, todoDueChange, todoDueOfMention } from "./todoTask";

/**
 * A row picked with ↵ in a to-do's "@" menu, as Notion's (seen 2026-11-09):
 * a date row writes its date in the line, a grey "@오늘" — a reminder's
 * without its alarm, which goes to the due date instead — and makes it the
 * to-do's due date; a person row writes "@〈이름〉" and assigns them, after
 * anyone already assigned. Text and task are one step of the page's history.
 * Someone the page doesn't open to is mentioned but not assigned: they
 * couldn't read what they would be given; nor is someone invited by an
 * address without an account yet, as Notion's to-do takes no such assignee
 * (seen 2026-11-12). Returns whether the mention was written.
 */
export function writeTodoMention({ adapter, run, block, item, write }: {
  adapter: NativeEditorGraphAdapter;
  run: (change: () => void) => void;
  block: GraphBlock;
  item: MentionItem & { kind: "date" | "person" };
  write: (spans: readonly TextSpan[]) => boolean;
}): boolean {
  const live = () => {
    const found = adapter.graph.blocks.find((candidate) => candidate.id === block.id);
    return found?.type === "to_do" ? found : null;
  };
  const insertion = mentionInsertion(item.kind === "date" ? { ...item, dateReminder: undefined } : item);
  if (!insertion) return false;
  const spans = [{ text: insertion.text, ...insertion.attributes }, ...(insertion.spaced ? [{ text: " " }] : [])];
  const me = adapter.mentionSnapshot?.currentUserId || adapter.session?.user.id || "";
  let wrote = false;
  run(() => {
    wrote = write(spans);
    const target = wrote ? live() : null;
    if (!target) return;
    if (item.kind === "date") adapter.updateBlock(target, todoDueChange(todoDueOfMention(item)));
    else if (item.userId === me) assign(target, item.userId);
  });
  if (!wrote || item.kind !== "person" || item.userId === me || !adapter.session || personIsInvitee(item.userId)) return wrote;
  const userId = item.userId;
  void pageUnreachablePeople(adapter.session, adapter.pageId, [userId]).then((unreachable) => {
    const target = live();
    if (!unreachable || unreachable.has(userId) || !target || adapter.readOnly || !adapter.isCurrent()) return;
    run(() => assign(target, userId));
  });
  return wrote;

  function assign(target: GraphBlock, userId: string) {
    const current = todoAssignees(target.content);
    if (!current.includes(userId)) adapter.updateBlock(target, todoAssigneesChange([...current, userId]));
  }
}

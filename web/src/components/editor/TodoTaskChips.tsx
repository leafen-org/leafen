import { useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import type { GraphBlock } from "@/contentGraph";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { AlarmIcon, CalendarIcon, UserIcon } from "@/icons/leafen";
import { personLabel } from "@/lib/peopleDirectory";
import { usePeopleDirectoryRevision } from "@/lib/workspacePeopleDirectory";
import { localIsoDate } from "./mentionCalendarModel";
import { blockItemText } from "./blockItemLabels";
import { DateMentionEditor, type DateMentionTarget } from "./DateMentionEditor";
import type { NativeEditorGraphAdapter } from "./editorRuntimeTypes";
import { TodoAssigneePicker } from "./TodoAssigneePicker";
import {
  todoAssignees, todoAssigneesChange, todoDue, todoDueChange, todoDueLabel, useTodoMenuPreview, type TodoDue,
} from "./todoTask";
import styles from "./TodoTaskChips.module.css";
import { inlineStartX } from "@/lib/readingDirection";

type TaskProps = {
  adapter: NativeEditorGraphAdapter;
  block: GraphBlock;
  /** Runs a change as one step of the page's history. */
  run: (change: () => void) => void;
};

/**
 * A to-do's text with its task ahead of it, which stands at the end of the
 * text's first line, as Notion's (measured 2026-11-16 at 1440 and 1024): the
 * lines under it run the text's whole width, and a text narrower than 300px
 * shows no task at all — in a column or nested alike (TodoTaskChips.module.css).
 */
export function TodoTaskLine({ children, ...task }: TaskProps & { children: ReactNode }) {
  return <div className={styles.line}><TodoTaskChips {...task} />{children}</div>;
}

/**
 * A to-do's task at its line's end, as Notion's (seen 2026-11-09): the faces
 * of the people it is assigned to, overlapping, then its due date in small
 * grey letters — blue with an alarm clock when it reminds — each a button
 * that opens its own list or date window. With either missing, a person and
 * a calendar button come out where the pointer rests at the line's end, the
 * hidden "담당자 추가" and "마감일 추가". While the to-do's "@" menu is open the
 * date or face its highlighted row would set shows there faded.
 */
function TodoTaskChips({ adapter, block, run }: TaskProps) {
  usePeopleDirectoryRevision();
  const assignees = todoAssignees(block.content);
  const due = todoDue(block.content);
  const preview = useTodoMenuPreview(block.id);
  const [open, setOpen] = useState<"people" | "date" | null>(null);
  const [dateTarget, setDateTarget] = useState<DateMentionTarget | null>(null);
  const peopleRef = useRef<HTMLButtonElement>(null);
  const addPeopleRef = useRef<HTMLButtonElement>(null);
  const rootRef = useRef<HTMLSpanElement>(null);
  const editable = !adapter.readOnly && Boolean(adapter.mentionSnapshot);
  const latest = useRef(block); latest.current = block;
  const live = () => adapter.graph.blocks.find((candidate) => candidate.id === latest.current.id) ?? latest.current;
  const change = (patch: Record<string, unknown>) => {
    if (adapter.readOnly || !adapter.isCurrent()) return;
    run(() => adapter.updateBlock(live(), patch));
  };
  // The date window stays by the button it opened from, and by the date
  // once "마감일 추가" has made one and gone.
  const openDate = (element: Element) => {
    const rect = () => (element.isConnected ? element : rootRef.current?.querySelector("[data-todo-due]") ?? element).getBoundingClientRect();
    const anchor = { contextElement: rootRef.current ?? element,
      get left() { return inlineStartX(rect(), 0, element); }, get top() { return rect().top; }, get bottom() { return rect().bottom; } };
    setDateTarget({ ordinal: 0, date: due?.date ?? localIsoDate(0), dateEnd: due?.dateEnd, dateTimeZone: due?.dateTimeZone,
      dateReminder: due?.dateReminder, anchor });
    setOpen("date");
  };
  const shownPeople = preview?.userId && !assignees.includes(preview.userId) ? [...assignees, preview.userId] : assignees;
  const shownDue: TodoDue | null = preview?.due ?? due;
  const names = assignees.map((id) => personLabel(id, adapter.mentionSnapshot?.currentUserId, false)).join(", ");
  if (!editable && !assignees.length && !due) return null;
  const addPerson = editable && !assignees.length && !preview?.userId, addDue = editable && !due && !preview?.due;
  return (
    <span ref={rootRef} className={styles.chips} contentEditable={false} data-todo-task data-editable={editable ? "true" : undefined}
      data-adds={addPerson || addDue ? "true" : undefined}>
      {addPerson ? (
        <button ref={addPeopleRef} type="button" className={styles.add} aria-label={blockItemText("todo.addAssignee")}
          onClick={() => setOpen("people")}><UserIcon size="small" aria-hidden="true" /></button>
      ) : null}
      {addDue ? (
        <button type="button" className={styles.add} aria-label={blockItemText("todo.addDueDate")}
          onClick={(event) => openDate(event.currentTarget)}><CalendarIcon size="small" aria-hidden="true" /></button>
      ) : null}
      {shownPeople.length ? (
        <button ref={peopleRef} type="button" className={styles.people} disabled={!editable}
          data-preview={preview?.userId && !assignees.includes(preview.userId) ? "true" : undefined}
          aria-label={blockItemText("todo.assignees", { names: names || preview?.name || "" })}
          onClick={() => setOpen("people")}>
          {shownPeople.map((id) => (
            <span key={id} className={styles.face}>
              <PersonAvatar userId={id} name={id === preview?.userId ? preview.name : undefined} size={18} />
            </span>
          ))}
        </button>
      ) : null}
      {shownDue ? (
        <button type="button" className={styles.due} disabled={!editable} data-todo-due
          data-reminder={shownDue.dateReminder ? "true" : undefined} data-preview={preview?.due ? "true" : undefined}
          aria-label={blockItemText("todo.due", { date: todoDueLabel(shownDue) })}
          onClick={(event) => openDate(event.currentTarget)}>
          <span className={styles.dueText}>{todoDueLabel(shownDue)}</span>
          {shownDue.dateReminder ? <AlarmIcon size="tiny" aria-hidden="true" /> : null}
        </button>
      ) : null}
      {open === "people" && editable && adapter.mentionSnapshot ? (
        <TodoAssigneePicker triggerRef={assignees.length ? peopleRef : addPeopleRef} assignees={assignees}
          snapshot={adapter.mentionSnapshot} session={adapter.session} pageId={adapter.pageId}
          onChange={(next) => change(todoAssigneesChange(next))} onClose={() => setOpen(null)} />
      ) : null}
      {open === "date" && dateTarget && editable ? createPortal(<DateMentionEditor target={dateTarget} formats={false}
        onChange={(next) => change(todoDueChange({ date: next.date,
          ...(next.dateEnd ? { dateEnd: next.dateEnd } : {}), ...(next.dateTimeZone ? { dateTimeZone: next.dateTimeZone } : {}),
          ...(next.dateReminder ? { dateReminder: next.dateReminder } : {}) }))}
        onClose={() => { setOpen(null); setDateTarget(null); }}
        onRemove={() => { change(todoDueChange(null)); setOpen(null); setDateTarget(null); }} />, document.body) : null}
    </span>
  );
}

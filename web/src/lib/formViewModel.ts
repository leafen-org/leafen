import type {
  DbProperty,
  FormConditionGroup,
  FormViewConfig,
  FormViewQuestion,
  PropertyType,
} from "./types";
import { pageHref } from "@/lib/navigation";
import { serverUrl } from "@/lib/serverOrigin";


const FORM_VIEW_WRITABLE_PROPERTY_TYPES = [
  "title",
  "rich_text",
  "number",
  "select",
  "multi_select",
  "status",
  "date",
  "checkbox",
  "url",
  "email",
  "phone",
  "person",
  "relation",
  "place",
  "files",
] as const satisfies readonly PropertyType[];

const writableTypes = new Set<PropertyType>(FORM_VIEW_WRITABLE_PROPERTY_TYPES);

export function isFormViewProperty(
  property: Pick<DbProperty, "type">,
): boolean {
  return writableTypes.has(property.type);
}

/**
 * A property a form offers as a question: one it answers, but not a status,
 * which Notion's form neither makes a question of nor lists (seen 2026-11-02);
 * a status question an older form holds is still answered.
 */
export function isFormQuestionOffered(
  property: Pick<DbProperty, "type">,
): boolean {
  return property.type !== "status" && isFormViewProperty(property);
}

export function createFormQuestion(
  property: DbProperty,
  position: number,
): FormViewQuestion {
  return {
    id: `question:${property.id}`,
    propertyId: property.id,
    label: property.name || property.id,
    description: property.description ?? "",
    required: false,
    hidden: false,
    syncWithPropertyName: true,
    ...(property.type === "rich_text" ? { longAnswer: false } : {}),
    ...(property.type === "select" || property.type === "status" || property.type === "multi_select"
      ? { optionsDisplay: "list" as const }
      : {}),
    position,
  };
}

/**
 * A question for a property the database already has, picked from "기존 속성",
 * keeps its own name: renaming it leaves the property's, until "속성 이름과
 * 동기화" is turned on (Notion's, seen 2026-11-12); a new question's property
 * takes its name.
 */
export function createExistingFormQuestion(property: DbProperty, position: number): FormViewQuestion {
  return { ...createFormQuestion(property, position), syncWithPropertyName: false };
}

/**
 * A question in step with its property is named by it, unless it keeps its
 * own name: renaming the property leaves that, while renaming the question
 * still renames the property, as Notion's API form "Title" question (seen
 * 2026-11-12: "Title" over the property "이름", then "PropA", sync on).
 */
export function formQuestionLabel(
  question: FormViewQuestion,
  property: Pick<DbProperty, "id" | "name"> | undefined,
) {
  return question.syncWithPropertyName && !question.ownLabel
    ? property?.name || property?.id || question.label
    : question.label;
}

export function formHref(token: string) {
  return `/form/${encodeURIComponent(token)}`;
}

/**
 * Where "양식 편집" goes: the form's view in its database, at the database's
 * own address with the view (/p/<database>?v=<view>), as a view link names
 * it, never the page holding it with the view (which still opens it for
 * older links). A place that names no database goes to its page.
 */
export function formEditHref(place: { pageId: string; databaseId?: string; viewId: string }) {
  return pageHref(place.databaseId || place.pageId, new URLSearchParams({ v: place.viewId }));
}

export function absoluteFormUrl(token: string) {
  const href = formHref(token);
  return typeof window === "undefined" ? href : serverUrl(href);
}

/** A question takes a condition group when its answer is a choice or a checkbox, as Notion's menu offers it (seen 2026-11-05). */
export function formQuestionTakesCondition(type: PropertyType | undefined) {
  return type === "select" || type === "status" || type === "multi_select" || type === "checkbox";
}

/** Whether a group's questions are asked, from the answer to its source; the server decides the same (formConditionMet). */
function formConditionMet(group: FormConditionGroup, answer: unknown) {
  if (group.checked !== undefined) return (answer === true) === group.checked;
  const wanted = group.optionIds ?? [];
  if (!wanted.length) return false;
  const chosen = Array.isArray(answer) ? answer : typeof answer === "string" ? [answer] : [];
  return group.matchAll ? wanted.every((id) => chosen.includes(id)) : wanted.some((id) => chosen.includes(id));
}

/** The questions a respondent is asked, given the answers so far: shown ones with every group above them holding. */
export function formAskedQuestions(form: Pick<FormViewConfig, "questions" | "conditionGroups">, values: Record<string, unknown>) {
  const groups = new Map((form.conditionGroups ?? []).map((group) => [group.id, group]));
  const questions = new Map(form.questions.map((question) => [question.id, question]));
  const holds = (question: FormViewQuestion): boolean => {
    for (let at: FormViewQuestion | undefined = question, depth = 0; at?.conditionGroupId && depth <= form.questions.length; depth += 1) {
      const group = groups.get(at.conditionGroupId);
      const source = group && questions.get(group.sourceQuestionId);
      if (!group || !source || !formConditionMet(group, values[source.propertyId])) return false;
      at = source;
    }
    return true;
  };
  return form.questions.filter((question) => !question.hidden && holds(question));
}

export type FormQuestionNode = { question: FormViewQuestion; groups: FormGroupNode[] };
export type FormGroupNode = { group: FormConditionGroup; nodes: FormQuestionNode[] };

/** The questions as the builder lays them out: each with its groups, and theirs in them. */
export function formQuestionTree(form: Pick<FormViewConfig, "questions" | "conditionGroups">): FormQuestionNode[] {
  const groups = form.conditionGroups ?? [];
  const known = new Set(groups.map(({ id }) => id));
  const nodeOf = (question: FormViewQuestion, path: ReadonlySet<string>): FormQuestionNode => ({
    question,
    groups: path.has(question.id) ? [] : groups.filter((group) => group.sourceQuestionId === question.id).map((group) => ({
      group,
      nodes: form.questions.filter((item) => item.conditionGroupId === group.id).map((item) => nodeOf(item, new Set([...path, question.id]))),
    })),
  });
  return form.questions.filter((question) => !question.conditionGroupId || !known.has(question.conditionGroupId))
    .map((question) => nodeOf(question, new Set()));
}

/** The form holding this layout: a group's questions right after their source, numbered in that order. */
export function formFromTree(form: FormViewConfig, tree: readonly FormQuestionNode[]): FormViewConfig {
  const questions: FormViewQuestion[] = [];
  const conditionGroups: FormConditionGroup[] = [];
  const walk = (nodes: readonly FormQuestionNode[], groupId?: string) => {
    for (const { question, groups } of nodes) {
      const { conditionGroupId: _old, ...rest } = question;
      questions.push({ ...rest, ...(groupId ? { conditionGroupId: groupId } : {}), position: questions.length } as FormViewQuestion);
      for (const { group, nodes: inner } of groups) {
        conditionGroups.push(group);
        walk(inner, group.id);
      }
    }
  };
  walk(tree);
  const { conditionGroups: _old, ...rest } = form;
  return { ...rest, questions, ...(conditionGroups.length ? { conditionGroups } : {}) };
}

/** The layout with a question taken out, and its node with whatever groups it heads. */
export function formTreeWithout(tree: readonly FormQuestionNode[], id: string): { tree: FormQuestionNode[]; taken: FormQuestionNode | null } {
  let taken: FormQuestionNode | null = null;
  const strip = (nodes: readonly FormQuestionNode[]): FormQuestionNode[] => nodes.flatMap((node) => {
    if (node.question.id === id) {
      taken = node;
      return [];
    }
    return [{ ...node, groups: node.groups.map((entry) => ({ ...entry, nodes: strip(entry.nodes) })) }];
  });
  return { tree: strip(tree), taken };
}

/** The layout with one group's questions changed, wherever it sits. */
export function formTreeMapGroups(tree: readonly FormQuestionNode[], change: (entry: FormGroupNode) => FormGroupNode | null): FormQuestionNode[] {
  return tree.map((node) => ({
    ...node,
    groups: node.groups.flatMap((entry) => {
      const next = change({ ...entry, nodes: formTreeMapGroups(entry.nodes, change) });
      return next ? [next] : [];
    }),
  }));
}

/** The layout with one question changed, wherever it sits; it may become several at its place. */
export function formTreeReplace(tree: readonly FormQuestionNode[], id: string, change: (node: FormQuestionNode) => FormQuestionNode[]): FormQuestionNode[] {
  return tree.flatMap((node) => node.question.id === id ? change(node) : [{
    ...node,
    groups: node.groups.map((entry) => ({ ...entry, nodes: formTreeReplace(entry.nodes, id, change) })),
  }]);
}

/** Whether a question's property fixes how many it takes: a relation of one page (Notion's "제한"). */
export function formQuestionMostFixed(property: { type: string; config?: unknown }) {
  return property.type === "relation" && (property.config as { limit?: unknown } | null | undefined)?.limit === 1;
}

/**
 * The most answers a question takes: its own most, held to its relation's
 * own limit — a relation of one page takes one, its "최대 선택 항목 수" set
 * at 1, as Notion's (seen 2026-11-06).
 */
export function formQuestionMostSelections(question: { maxSelections?: number }, property: { type: string; config?: unknown }) {
  return formQuestionMostFixed(property) ? 1 : question.maxSelections;
}

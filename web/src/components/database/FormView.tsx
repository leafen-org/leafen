"use client";

import { useProductPages } from "@/components/product/productPagesContext";
import { takeFreshForm } from "@/lib/freshDatabases";
import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { ArrowRight, FormBlockIcon, FormPreviewEyeIcon, Plus, TableIcon } from "@/icons/leafen";
import { MonitorIcon } from "@/icons/leafen/deviceIcons";
import { confirmAction } from "../ConfirmActionDialog";
import type { GraphDataSource, GraphProperty, GraphView } from "@/contentGraph";
import type { FormSubmitterAccess } from "@/formWire.generated";
import { useCopyNotice } from "@/lib/copyNotice";
import {
  createDefaultFormViewConfig,
  formRequiresWorkspaceAudience,
} from "@/lib/formViewProjection";
import {
  absoluteFormUrl,
  createExistingFormQuestion,
  createFormQuestion,
  formFromTree,
  formQuestionLabel,
  formQuestionTree,
  formTreeMapGroups,
  formTreeReplace,
  formTreeWithout,
  isFormQuestionOffered,
  type FormQuestionNode,
} from "@/lib/formViewModel";
import { newId } from "@/lib/ids";
import { lendFormSettings } from "@/lib/formSettingsBridge";
import { LocalizedError } from "@/lib/settingsErrors";
import type { NativeFormService } from "@/lib/nativeFormService";
import { publicFormFailureMessage } from "@/lib/publicFormView";
import type {
  DbProperty,
  FormAudience,
  FormConditionGroup,
  FormViewConfig,
  FormViewQuestion,
  PropertyConfig,
} from "@/lib/types";
import { FormCover, FormHeader, FormQuestionFields, FormRespondent, FormSubmitButton } from "../forms/FormSurface";
import { FormAddQuestionMenu, formQuestionTypeMadeHere, newQuestionName, type NewQuestionType } from "./FormAddQuestionMenu";
import { FormHeadCover, FormHeadDecor, type FormDecorChange } from "./FormHeadDecor";
import { FormBuilderColumn } from "./FormBuilderColumn";
import { FormConditionGroupView } from "./FormConditionGroup";
import type { DatabasePropertyEditing } from "./DatabasePropertyEditPanel";
import { FormPreviewDialog } from "./FormPreviewDialog";
import { FormUnavailableNotice, PublicFormView } from "../PublicFormView";
import { FormQuestionCard, type FormQuestionKind, type FormQuestionMove } from "./FormQuestionCard";
import { FormQuestionGap, FormQuestionSlot } from "./FormQuestionGap";
import { FormShareMenu } from "./FormShareMenu";
import { audienceForView, FormAudienceNotice, formIsNew, submitterAccessForView } from "./FormAudienceControls";
import type { FormSurfaceBounds } from "../forms/formSurfaceBudget";
import styles from "./FormView.module.css";
import { disambiguatePropertyLabels } from "./propertyOptionLabels";
import { propertyAfterTypeChange } from "./propertyEditModel";
import { inPublicOrder, publicOrderOf } from "./propertyPublicOrder";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

type FormViewProps = {
  bounds: FormSurfaceBounds;
  dataSource: GraphDataSource;
  formService?: NativeFormService;
  onAddProperty?: (
    source: GraphDataSource,
    type: GraphProperty["type"],
    name: string,
    config?: Record<string, unknown>,
  ) => Promise<GraphProperty | null>;
  onConfiguredView?: (view: GraphView) => void;
  /** Saves a property the builder changed, as a choice question's options. */
  onUpdateProperty?: (property: GraphProperty) => void;
  pageId: string;
  properties: GraphProperty[];
  readOnly: boolean;
  view: GraphView;
  /** The view's row, where "미리보기" and "양식 공유" go as Notion's; without it they lead the builder. */
  barSlot?: HTMLElement | null;
  /** The signed-in person, whom the preview shows answering, as Notion's does. */
  respondent?: { id: string; name: string };
  /** A question's "연결된 속성 보기" edits its property here. */
  propertyEditing?: DatabasePropertyEditing;
};

function cloneConfig(config: FormViewConfig): FormViewConfig {
  return typeof structuredClone === "function"
    ? structuredClone(config)
    : JSON.parse(JSON.stringify(config)) as FormViewConfig;
}

function graphPropertyForForm(property: GraphProperty, position: number): DbProperty {
  const description = typeof property.config.description === "string"
    ? property.config.description
    : undefined;
  return {
    id: property.id,
    databaseId: property.dataSourceId,
    name: property.name,
    ...(description ? { description } : {}),
    type: property.type,
    config: property.config as PropertyConfig,
    position,
    ...(property.publicOrder !== undefined ? { publicOrder: property.publicOrder } : {}),
  };
}

// A new form's title is empty, "양식 제목" until written, as Notion's
// (seen 2026-10-06 and 2026-10-30), whatever its database is called. Its
// questions ask the title first, then each property in the order the API
// lists them (publicOrder), as "질문 추가" offers them — Notion's "질문 N개
// 만들기" (seen 2026-11-15: 이름, 태그, 선택, 날짜, 체크, 글, 수 from IDs
// "C]_L" "I]bU" "Pg][" "W\SU" "]bB{" "k}nd", neither their names' nor their
// columns' order; 2026-11-12 on another database alike).
function formForView(view: GraphView, properties: DbProperty[]) {
  const configured = view.config.leafenForm;
  if (configured) return cloneConfig(configured as FormViewConfig);
  const asked = [...properties].sort((a, b) => Number(b.type === "title") - Number(a.type === "title")
    || publicOrderOf(a) - publicOrderOf(b) || a.position - b.position).map((property, position) => ({ ...property, position }));
  const form = createDefaultFormViewConfig(asked, { title: "" });
  if (!madeWithTitleAlone(view)) return form;
  const titles = new Set(properties.filter(({ type }) => type === "title").map(({ id }) => id));
  const kept = { ...form, questions: form.questions.filter(({ propertyId }) => titles.has(propertyId))
    .map((question) => ({ ...question, label: API_FORM_TITLE_QUESTION, syncWithPropertyName: true, ownLabel: true })) };
  return formFromTree(kept, formQuestionTree(kept));
}

// A form the API made opens on its builder with its title's question alone,
// asking nothing, as Notion's (seen 2026-11-12); the server marks it so. The
// question is named "Title" in every language, whatever the property is
// called, and keeps that name when the property is renamed, its "속성 이름과
// 동기화" on all the while (Notion's, seen 2026-11-12 in Korean: "Title" over
// the property "이름", still so once it was "제목칸" or "PropA", and once the
// sync was turned off and on); a form made on the screen names it after the
// property.
const API_FORM_TITLE_QUESTION = "Title";
function madeWithTitleAlone(view: GraphView) {
  return view.config.leafenFormStart === "title";
}

// Changes save on their own after a short pause, as Notion's builder keeps them.
const AUTOSAVE_DELAY_MS = 700;

export function FormView({
  bounds,
  dataSource,
  formService,
  onAddProperty,
  onConfiguredView,
  onUpdateProperty,
  pageId,
  properties: graphProperties,
  readOnly,
  view,
  barSlot = null,
  respondent,
  propertyEditing,
}: FormViewProps) {
  const { t } = useTranslation(["formView", "databaseView", "common", "blockHandle"]);
  const copyNotice = useCopyNotice();
  const productPages = useProductPages();
  const workspaceName = productPages?.workspaces?.find(({ id }) => id === productPages.workspaceId)?.name ?? "";
  const service = formService;
  const properties = useMemo(
    () => graphProperties.map(graphPropertyForForm),
    [graphProperties],
  );
  const defaultForm = useMemo(
    () => formForView(view, properties),
    [properties, view],
  );
  const [draft, setDraft] = useState(defaultForm);
  const titleRef = useRef<HTMLTextAreaElement>(null);
  useEffect(() => { if (takeFreshForm(view.id)) titleRef.current?.focus({ preventScroll: true }); }, [view.id]);
  const [audience, setAudience] = useState<FormAudience>(() => audienceForView(view));
  const [submitterAccess, setSubmitterAccess] = useState<FormSubmitterAccess>(() => submitterAccessForView(view));
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [previewValues, setPreviewValues] = useState<Record<string, unknown>>({});
  const [previewLink, setPreviewLink] = useState<{ viewId: string; url: string; token: string } | null>(null);
  const configurationMutationIdRef = useRef("");
  // Each change counts up; a save that ends after a newer change keeps it.
  const versionRef = useRef(0);
  // The draft version whose save the server refused.
  const failedVersionRef = useRef(-1);
  const [previewing, setPreviewing] = useState(false);
  const [shareOpen, setShareOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  // The gap, by the question after it, whose "질문 추가" is open (FormQuestionGap).
  const [gapAt, setGapAt] = useState<number | null>(null);
  const shareButtonRef = useRef<HTMLButtonElement>(null), changeAudienceRef = useRef<HTMLButtonElement>(null);
  const [shareAnchor, setShareAnchor] = useState<"bar" | "notice">("bar");
  const addButtonRef = useRef<HTMLButtonElement>(null);
  const addSlotRef = useRef<HTMLDivElement>(null);
  // A question just made from "+" opens with its title chosen, as Notion's.
  const [madeQuestionId, setMadeQuestionId] = useState("");
  // A question moved to a new property by "질문 유형" keeps its options open
  // while its card comes back with the property (Notion's, seen 2026-11-08).
  const [retypedQuestionId, setRetypedQuestionId] = useState("");
  // The condition group whose choice of a question is open.
  const [addingGroupId, setAddingGroupId] = useState("");
  // A save's view comes back a render after the save ends (the page's graph
  // reaches its blocks then). Until it does, the view still holds the form the
  // save replaced: the draft keeps what was saved, and a form saved for the
  // first time isn't read as new (2026-11-11: in that render its start
  // question opened over the preview, and Escape there took its questions away).
  const [savedOver, setSavedOver] = useState<{ form: unknown } | null>(null);
  const viewBehind = savedOver !== null && view.config.leafenForm === savedOver.form;

  useEffect(() => {
    if (dirty || viewBehind) return;
    setSavedOver(null);
    setDraft(defaultForm);
    setAudience(audienceForView(view));
    setSubmitterAccess(submitterAccessForView(view));
  }, [defaultForm, dirty, view, viewBehind]);

  const propertiesById = useMemo(
    () => new Map(properties.map((property) => [property.id, property])),
    [properties],
  );
  useEffect(() => {
    const retyped = draft.questions.find(({ id }) => id === retypedQuestionId);
    if (retyped && propertiesById.has(retyped.propertyId)) setRetypedQuestionId("");
  }, [draft.questions, propertiesById, retypedQuestionId]);
  const usedIds = useMemo(
    () => new Set(draft.questions.map((question) => question.propertyId)),
    [draft.questions],
  );
  // The properties without a question in the order the API lists them, by
  // their public IDs, as Notion's "질문 추가" (seen 2026-11-12); the server
  // tells each one's place (publicOrder), one not told yet comes last.
  const availableProperties = useMemo(
    () => inPublicOrder(properties.filter((property) => isFormQuestionOffered(property) && !usedIds.has(property.id))),
    [properties, usedIds]);
  const availablePropertyLabels = useMemo(
    () => disambiguatePropertyLabels(availableProperties, t("databaseView:untitled")),
    [availableProperties, t],
  );
  const configurationReady = Boolean(service && onConfiguredView);
  // Whether a form was set up is the server's to say: a copy of the page kept
  // here may be older than a form saved since (2026-11-13: a saved form asked
  // its first question again on opening, and an answer would have saved over it).
  const viewsRead = service?.viewsRead !== false;
  // A new form made on the screen asks in a card (Notion's, seen 2026-10-30) whether each property after the title that
  // a form answers becomes a question — not asked for none (2026-11-25, two tabs: title alone, status, formula); "처음부터
  // 만들기" or a press around the card keeps the title's, Escape leaves it asking.
  const startAsked = useRef(false);
  const formSetUp = useRef(false);
  formSetUp.current = Boolean(view.config.leafenForm);
  // Until it is answered the builder shows the form's head alone, as Notion's (seen 2026-11-02).
  const [startPending, setStartPending] = useState(false);
  const askable = properties.filter((property) => property.type !== "title" && isFormQuestionOffered(property));
  const startAsking = !readOnly && !view.config.leafenForm && !dirty && !viewBehind && configurationReady && viewsRead && askable.length > 0
    && !madeWithTitleAlone(view);
  // A new form with nothing to ask saves at once, so it is open as it says.
  useEffect(() => {
    if (!readOnly && configurationReady && viewsRead && formIsNew(view) && (askable.length === 0 || madeWithTitleAlone(view)) && !dirty) changeDraft((form) => form);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [configurationReady, viewsRead, view.id]);
  useEffect(() => {
    if (!startAsking || startAsked.current) return;
    startAsked.current = true;
    setStartPending(true);
    void confirmAction({
      icon: <span className={styles.startMarks}><TableIcon size="regular" /><ArrowRight size="regular" /><FormBlockIcon size="regular" /></span>,
      message: t("formView:startQuestion"), detail: t("formView:startDetail"),
      confirmLabel: t("formView:startWithQuestions", { count: askable.length }), cancelLabel: t("formView:startEmpty"), primary: true,
      returnFocus: false, staysOnEscape: true,
    }).then((all) => {
      setStartPending(false);
      // A form the server answered with meanwhile stays as it is.
      if (formSetUp.current) return;
      // The title's question alone, numbered again from the top (the server
      // takes 0 to one less than the questions: 2026-11-06, the title at 4 was
      // refused).
      changeDraft((form) => {
        if (all) return form;
        const kept = { ...form, questions: form.questions.filter((question) => propertiesById.get(question.propertyId)?.type === "title") };
        return formFromTree(kept, formQuestionTree(kept));
      });
    });
  }, [startAsking]);
  const requiresWorkspaceAudience = formRequiresWorkspaceAudience(draft, properties);
  const audienceCompatible = !requiresWorkspaceAudience || audience !== "web";

  function changeDraft(update: (current: FormViewConfig) => FormViewConfig) {
    configurationMutationIdRef.current = "";
    versionRef.current += 1;
    setDraft((current) => update(current));
    setDirty(true);
    setStatus("");
    setError("");
  }

  // "폼 설정" and its "제출 화면" in the view's settings change this form (lib/formSettingsBridge).
  useEffect(() => {
    if (readOnly) return;
    lendFormSettings(view.id, { form: draft, change: changeDraft, addQuestion: () => {
      addButtonRef.current?.scrollIntoView({ block: "center" });
      setAddOpen(true);
    } });
    // changeDraft only reaches refs and setters, so the form alone renews it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft, readOnly, view.id]);
  useEffect(() => () => lendFormSettings(view.id, null), [view.id]);

  // The icon and cover; a cover's place is kept off its middle only.
  function changeDecor(change: FormDecorChange) {
    changeDraft((form) => {
      const { coverPosition: before, ...rest } = form;
      const { coverPosition: asked, ...set } = change;
      const next: FormViewConfig = { ...rest, ...set };
      const place = asked ?? before;
      return next.cover && place !== undefined && place !== 50 ? { ...next, coverPosition: place } : next;
    });
  }

  function updateQuestion(id: string, patch: Partial<FormViewQuestion>) {
    changeDraft((current) => ({
      ...current,
      questions: current.questions.map((question) => question.id === id ? { ...question, ...patch } : question),
    }));
  }

  // A question moves with its condition groups, among the questions at its level.
  function moveQuestion(id: string, to: FormQuestionMove) {
    changeTree((tree) => {
      const reorder = (nodes: FormQuestionNode[]): FormQuestionNode[] => {
        const index = nodes.findIndex((node) => node.question.id === id);
        if (index < 0) return nodes.map((node) => ({ ...node, groups: node.groups.map((entry) => ({ ...entry, nodes: reorder(entry.nodes) })) }));
        const nextIndex = to === "first" ? 0 : to === "last" ? nodes.length - 1 : index + to;
        if (nextIndex < 0 || nextIndex >= nodes.length || nextIndex === index) return nodes;
        const next = nodes.slice();
        const [moved] = next.splice(index, 1);
        next.splice(nextIndex, 0, moved);
        return next;
      };
      return reorder(tree);
    });
  }

  function changeTree(change: (tree: FormQuestionNode[]) => FormQuestionNode[]) {
    changeDraft((current) => formFromTree(current, change(formQuestionTree(current))));
  }

  // "조건부 로직 추가": a new group under its question, and its choice of a question opens.
  function addConditionGroup(question: FormViewQuestion) {
    const group: FormConditionGroup = { id: `condition:${newId()}`, sourceQuestionId: question.id };
    changeTree((tree) => formTreeReplace(tree, question.id, (node) => [{ ...node, groups: [...node.groups, { group, nodes: [] }] }]));
    setAddingGroupId(group.id);
  }

  // A question goes into the group with whatever groups it heads.
  function intoGroup(groupId: string, question: FormViewQuestion) {
    changeTree((tree) => {
      const { tree: rest, taken } = formTreeWithout(tree, question.id);
      const node = taken ?? { question, groups: [] };
      return formTreeMapGroups(rest, (entry) => entry.group.id === groupId ? { ...entry, nodes: [...entry.nodes, node] } : entry);
    });
    setAddingGroupId("");
  }

  // Back at the level of the group's question, right after it.
  function leaveGroup(question: FormViewQuestion) {
    changeTree((tree) => {
      const { tree: rest, taken } = formTreeWithout(tree, question.id);
      if (!taken) return tree;
      const source = draft.conditionGroups?.find(({ id }) => id === question.conditionGroupId)?.sourceQuestionId ?? "";
      return formTreeReplace(rest, source, (node) => [node, taken]);
    });
  }

  // "질문 유형": the property takes the kind, and the question drops what only
  // the old kind offers. The title stays the database's: its question goes to
  // a new property of the kind, named for it, keeping its words; turned back
  // to "제목" it asks the title again and the property it leaves goes, as
  // Notion's (seen 2026-11-08: "질문 1" as 이메일 made an "이메일" column, and
  // back again took it away; a relation turned to text in place).
  async function changeQuestionType(question: FormViewQuestion, type: FormQuestionKind) {
    const graph = graphProperties.find(({ id }) => id === question.propertyId);
    const title = properties.find((property) => property.type === "title");
    if (!graph || !onUpdateProperty || (type === "title" && !title)) return;
    const label = formQuestionLabel(question, graphPropertyForForm(graph, 0));
    let moved: Partial<FormViewQuestion> = {};
    if (type === "title") moved = { propertyId: title!.id, label, syncWithPropertyName: label === title!.name };
    else if (graph.type === "title") {
      if (saving || !onAddProperty) return;
      const made = await onAddProperty(dataSource, type, newQuestionName(type, properties.map(({ name }) => name)));
      if (!made) return;
      moved = { propertyId: made.id, label, syncWithPropertyName: false };
      setRetypedQuestionId(question.id);
    } else onUpdateProperty(propertyAfterTypeChange(graph, type as GraphProperty["type"], dataSource.id));
    if (type === "title") propertyEditing?.onDeleteProperty?.(graph.id);
    const reads = type === "checkbox" || type === "multi_select";
    changeTree((tree) => formTreeReplace(tree, question.id, (node) => {
      const { longAnswer: _long, optionsDisplay: _display, maxSelections: _most, includeEndDate: _end, includeTime: _time, checkboxLabel: _box, ...rest } = node.question;
      const kept = { ...rest, ...moved };
      // A kind no condition can read lets its groups go, their questions staying as questions.
      return reads ? [{ ...node, question: kept as FormViewQuestion, groups: node.groups.map(({ group, nodes }) => ({ group: { id: group.id, sourceQuestionId: group.sourceQuestionId }, nodes })) }]
        : [{ question: kept as FormViewQuestion, groups: [] }, ...node.groups.flatMap(({ nodes }) => nodes)];
    }));
  }

  async function addNewToGroup(groupId: string, type: NewQuestionType) {
    setAddingGroupId("");
    if (saving || !onAddProperty) return;
    const graphProperty = await onAddProperty(dataSource, type, newQuestionName(type, properties.map((property) => property.name)));
    if (!graphProperty) return;
    const question = createFormQuestion(graphPropertyForForm(graphProperty, properties.length), 0);
    setMadeQuestionId(question.id);
    intoGroup(groupId, question);
  }

  // A new question goes last, or before the question `at` of the form's top level from a gap's "질문 추가".
  function placeQuestion(question: FormViewQuestion, at?: number) {
    if (at === undefined) changeDraft((current) => ({ ...current, questions: [...current.questions, { ...question, position: current.questions.length }] }));
    else changeTree((tree) => [...tree.slice(0, at), { question, groups: [] }, ...tree.slice(at)]);
  }

  function addExistingQuestion(propertyId: string, at?: number) {
    const property = propertiesById.get(propertyId);
    setAddOpen(false);
    setGapAt(null);
    if (!property) return;
    placeQuestion(createExistingFormQuestion(property, 0), at);
    setMadeQuestionId(`question:${property.id}`);
  }

  async function addNewQuestion(type: NewQuestionType, at?: number) {
    setAddOpen(false);
    setGapAt(null);
    if (saving || !onAddProperty) return;
    const graphProperty = await onAddProperty(dataSource, type, newQuestionName(type, properties.map((property) => property.name)));
    if (!graphProperty) return;
    const question = createFormQuestion(graphPropertyForForm(graphProperty, properties.length), 0);
    setMadeQuestionId(question.id);
    placeQuestion(question, at);
  }

  async function duplicateQuestion(question: FormViewQuestion) {
    const source = propertiesById.get(question.propertyId);
    if (!source || source.type === "title" || saving || !onAddProperty) return;
    const copyName = t("databaseView:copyName", { name: source.name });
    const config = source.config
      ? (typeof structuredClone === "function" ? structuredClone(source.config) : JSON.parse(JSON.stringify(source.config)))
      : undefined;
    const graphProperty = await onAddProperty(
      dataSource,
      source.type,
      copyName,
      config as Record<string, unknown> | undefined,
    );
    if (!graphProperty) return;
    const created = graphPropertyForForm(graphProperty, properties.length);
    changeDraft((current) => {
      const index = current.questions.findIndex((item) => item.id === question.id);
      const duplicate = {
        ...createFormQuestion(created, index + 1),
        description: question.description,
        required: question.required,
        longAnswer: question.longAnswer,
        optionsDisplay: question.optionsDisplay,
        maxSelections: question.maxSelections,
        includeEndDate: question.includeEndDate, includeTime: question.includeTime,
        checkboxLabel: question.checkboxLabel,
        conditionGroupId: question.conditionGroupId,
      };
      const questions = current.questions.slice();
      questions.splice(index + 1, 0, duplicate);
      return formFromTree(current, formQuestionTree({ ...current, questions }));
    });
  }

  async function save(copyLinkAfter = false) {
    if (!configurationReady || !audienceCompatible || saving || !service || !onConfiguredView) return "";
    setSaving(true);
    setError("");
    setStatus("");
    if (!configurationMutationIdRef.current) configurationMutationIdRef.current = newId();
    const version = versionRef.current;
    failedVersionRef.current = -1;
    try {
      const result = await service.configure({
        mutationId: configurationMutationIdRef.current,
        pageId,
        dataSourceId: dataSource.id,
        viewId: view.id,
        form: draft,
        audience,
        submitterAccess,
      });
      onConfiguredView({
        ...view,
        config: {
          ...view.config,
          leafenForm: result.form,
          leafenFormAudience: result.formLink.audience,
          leafenFormSubmitterAccess: result.formLink.submitterAccess,
        },
      });
      setSavedOver({ form: view.config.leafenForm });
      // A change made while this saved stays, and saves next.
      if (version === versionRef.current) {
        setDraft(cloneConfig(result.form));
        setAudience(result.formLink.audience);
        setSubmitterAccess(result.formLink.submitterAccess);
        setDirty(false);
        configurationMutationIdRef.current = "";
      }
      const nextPreviewUrl = result.formLink.enabled
        ? absoluteFormUrl(result.formLink.token)
        : "";
      setPreviewLink(nextPreviewUrl ? { viewId: result.formLink.viewId, url: nextPreviewUrl, token: result.formLink.token } : null);
      if (copyLinkAfter && result.formLink.enabled) {
        // Said in a toast, copied or not (lib/copyNotice), not in the bar's save state.
        await copyNotice(nextPreviewUrl, t("formView:linkCopied"));
      }
      return nextPreviewUrl;
    } catch (cause) {
      failedVersionRef.current = version;
      setError(cause instanceof LocalizedError ? cause.message : publicFormFailureMessage(cause, "configuration.write"));
      return "";
    } finally {
      setSaving(false);
    }
  }

  // A change saves once the builder rests; one the server refused waits for
  // the next change rather than going again every rest (2026-11-06: a refused
  // form went 50 times in half a minute).
  useEffect(() => {
    if (!dirty || saving || !configurationReady || !audienceCompatible || failedVersionRef.current === versionRef.current) return;
    const timer = window.setTimeout(() => { void save(false); }, AUTOSAVE_DELAY_MS);
    return () => window.clearTimeout(timer);
    // save reads the draft it saves; the timer restarts as the draft changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dirty, saving, draft, audience, submitterAccess, configurationReady, audienceCompatible]);

  // "양식 공유" shows the form's link, as Notion's; opened before one was read, a save reads it.
  const link = previewLink?.viewId === view.id ? previewLink.url : "";
  const previewToken = previewLink?.viewId === view.id ? previewLink.token : "";
  useEffect(() => {
    if (shareOpen && !link && audience !== "none") void save(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shareOpen]);

  // "미리보기" is the form as its respondents have it, sending a real answer,
  // as Notion's (seen 2026-11-04: "보내기" made a row and said "고마워요"); it
  // shows what is saved, so a change saves first. A form no one may fill
  // shows its questions alone.
  async function openPreview() {
    if (dirty || (!link && audience !== "none")) await save(false);
    setPreviewing(true);
  }

  // Who may answer and what they see after: a new version to save.
  function changeLinkSetting(apply: () => void) {
    configurationMutationIdRef.current = "";
    versionRef.current += 1;
    apply();
    setDirty(true);
    setStatus("");
  }

  const preview = (
    <div className={styles.previewCard} data-form-preview>
      <FormCover form={draft} />
      <FormHeader form={draft} bounds={bounds} />
      {respondent ? <FormRespondent respondent={respondent} /> : null}
      {draft.questions.length === 0 && <p className={styles.empty}>{t("formView:noQuestions")}</p>}
      <FormQuestionFields
        form={draft}
        bounds={bounds}
        properties={properties}
        values={previewValues}
        disabled={readOnly}
        onChange={(propertyId, value) => setPreviewValues((current) => ({ ...current, [propertyId]: value }))}
      />
      <FormSubmitButton form={draft} bounds={bounds} disabled />
    </div>
  );

  if (readOnly) return <div className={styles.readOnly}>{preview}</div>;

  const tree = formQuestionTree(draft);
  // No question asks the title, which "질문 유형" then offers back.
  const titleFree = properties.some(({ type }) => type === "title")
    && !draft.questions.some(({ propertyId }) => propertiesById.get(propertyId)?.type === "title");
  // The questions a group can take in: any other but its source and what the source sits under.
  const groupCandidates = (group: FormConditionGroup, entryNodes: readonly FormQuestionNode[]) => {
    const taken = new Set(entryNodes.map(({ question }) => question.id));
    const above = new Set<string>();
    for (let at = group.sourceQuestionId, depth = 0; at && depth <= draft.questions.length; depth += 1) {
      above.add(at);
      const inGroup = draft.questions.find(({ id }) => id === at)?.conditionGroupId;
      at = draft.conditionGroups?.find(({ id }) => id === inGroup)?.sourceQuestionId ?? "";
    }
    return draft.questions.filter(({ id }) => !taken.has(id) && !above.has(id)).flatMap((item) => {
      const target = propertiesById.get(item.propertyId);
      return target ? [{ id: item.id, name: formQuestionLabel(item, target), type: target.type }] : [];
    });
  };
  // The form's top level has a "질문 추가" in the gap before each question (FormQuestionGap).
  const canAdd = !readOnly && (availableProperties.length > 0 || Boolean(onAddProperty));
  const gapBefore = (at: number) => (
    <FormQuestionGap open={gapAt === at} onOpen={() => setGapAt(at)} menu={(anchor) => (
      <FormAddQuestionMenu anchor={anchor} beside besideGap={-22} properties={availableProperties} labels={availablePropertyLabels}
        canCreate={Boolean(onAddProperty)} onExisting={(propertyId) => addExistingQuestion(propertyId, at)}
        onNew={(type) => void addNewQuestion(type, at)} onClose={() => setGapAt(null)} />
    )} />
  );
  const renderNodes = (nodes: readonly FormQuestionNode[], gaps = false) => nodes.map(({ question, groups }, index) => {
    const property = propertiesById.get(question.propertyId);
    if (!property) return null;
    return (
      <Fragment key={question.id}>
        {gaps && canAdd ? gapBefore(index) : null}
        {questionCard(question, property, { index, count: nodes.length })}
        {groups.map(({ group, nodes: inner }) => (
          <FormConditionGroupView key={group.id} group={group} property={property} count={inner.length} adding={addingGroupId === group.id}
            onChange={(patch) => changeDraft((form) => ({
              ...form,
              conditionGroups: form.conditionGroups?.map((item) => {
                if (item.id !== group.id) return item;
                const next = { ...item, ...patch };
                if (next.checked === undefined) delete next.checked;
                if (next.matchAll !== true) delete next.matchAll;
                return next;
              }),
            }))}
            // The bin takes the group and the questions in it, as Notion's (seen 2026-11-05).
            onRemove={() => changeTree((current) => formTreeMapGroups(current, (entry) => entry.group.id === group.id ? null : entry))}
            onAdding={(open) => setAddingGroupId(open ? group.id : "")}
            addMenu={(anchor) => (
              <FormAddQuestionMenu anchor={anchor} beside properties={availableProperties} labels={availablePropertyLabels}
                canCreate={Boolean(onAddProperty)} questions={groupCandidates(group, inner)}
                onQuestion={(id) => {
                  const moving = draft.questions.find((item) => item.id === id);
                  if (moving) intoGroup(group.id, moving);
                }}
                onExisting={(propertyId) => {
                  const target = propertiesById.get(propertyId);
                  if (target) intoGroup(group.id, createExistingFormQuestion(target, 0));
                }}
                onNew={(type) => void addNewToGroup(group.id, type)}
                onClose={() => setAddingGroupId("")} />
            )}>
            {renderNodes(inner)}
          </FormConditionGroupView>
        ))}
      </Fragment>
    );
  });
  const questionCard = (question: FormViewQuestion, property: DbProperty, place: { index: number; count: number }) => (
    <FormQuestionCard question={question} property={property} index={place.index} count={place.count}
      canDuplicate={formQuestionTypeMadeHere(property.type) && Boolean(onAddProperty)} focusTitle={question.id === madeQuestionId}
      menuOpenAtStart={question.id === retypedQuestionId}
      editing={propertyEditing} graphProperty={graphProperties.find(({ id }) => id === property.id)}
      onChange={(patch) => updateQuestion(question.id, patch)} onMove={(to) => moveQuestion(question.id, to)}
      onOptionsChange={onUpdateProperty && !readOnly ? (options) => {
        const graph = graphProperties.find((item) => item.id === property.id);
        if (graph) onUpdateProperty({ ...graph, config: { ...graph.config, options } });
      } : undefined}
      onRename={onUpdateProperty && !readOnly ? (name) => {
        const graph = graphProperties.find((item) => item.id === property.id);
        if (!graph || graphProperties.some((item) => item.id !== property.id && item.name.trim() === name.trim())) return false;
        onUpdateProperty({ ...graph, name });
        return true;
      } : undefined}
      onDuplicate={() => void duplicateQuestion(question)}
      // A question goes with the groups under it and their questions, as Notion's (seen 2026-11-05).
      onRemove={() => changeTree((nodes) => formTreeWithout(nodes, question.id).tree)}
      onChangeType={onUpdateProperty ? (type) => void changeQuestionType(question, type) : undefined} titleFree={titleFree}
      onAddCondition={() => addConditionGroup(question)}
      onLeaveGroup={question.conditionGroupId ? () => leaveGroup(question) : undefined} />
  );

  const bar = (
    <div className={barSlot ? styles.rowBar : styles.builderBar} data-form-bar>
      {saving ? <span className={styles.saveState} role="status">{t("formView:saving")}</span>
        : status ? <span className={styles.saveState} role="status">{status}</span> : null}
      {!configurationReady ? <span className={styles.saveState}>{t("formView:setupPending")}</span> : null}
      <button type="button" className={styles.barButton} aria-haspopup="dialog" aria-expanded={previewing} data-form-preview-toggle
        onClick={() => void openPreview()}>
        <FormPreviewEyeIcon aria-hidden="true" />{t("formView:preview")}
      </button>
      <button ref={shareButtonRef} type="button" className={styles.primary} aria-haspopup="dialog" aria-expanded={shareOpen}
        data-form-share onClick={() => { setShareAnchor("bar"); setShareOpen((open) => !open); }}>
        <span className={styles.shareLong}>{t("formView:shareForm")}</span><span className={styles.shareShort}>{t("databaseView:share")}</span>
      </button>
    </div>
  );

  return (
    <div className={styles.root} data-form-builder>
      {/* A touch screen, a tablet's as a phone's, edits no form: the view says so under a screen, its
          "미리보기" and "공유" kept beside the tabs (Notion's iPad and iPhone, seen 2026-11-13). */}
      <div className={styles.desktopOnly} role="note">
        <MonitorIcon size="empty" aria-hidden="true" />
        <span>{t("formView:desktopOnlyEditing")}</span>
      </div>
      <div className={styles.desktopBuilder}>
        {barSlot ? createPortal(bar, barSlot) : bar}
        {shareOpen ? (
          <FormShareMenu anchor={shareAnchor === "bar" ? shareButtonRef : changeAudienceRef} audience={audience}
            anonymous={draft.anonymous === true} onAnonymous={(anonymous) => changeDraft((form) => {
              const { anonymous: _old, ...rest } = form;
              return anonymous ? { ...rest, anonymous: true } : rest;
            })}
            submitterAccess={submitterAccess} webDisabled={requiresWorkspaceAudience} canCopy={configurationReady && audienceCompatible}
            saving={saving} link={link} workspaceName={workspaceName} status={status} error={error}
            onAudience={(next) => changeLinkSetting(() => setAudience(next))} onSubmitterAccess={(next) => changeLinkSetting(() => setSubmitterAccess(next))}
            onCopy={() => void save(true)} onClose={() => setShareOpen(false)} />
        ) : null}
        {!audienceCompatible ? <p className={styles.error} role="alert">{t("formView:referenceAudienceRequired")}</p> : null}
        {error && !shareOpen ? <p className={styles.error} role="alert">{error}</p> : null}
        {previewing ? <FormPreviewDialog token={previewToken || undefined} onClose={() => setPreviewing(false)}
          onOpenFull={configurationReady && audienceCompatible ? () => void save(false).then((url) => { if (url) window.location.assign(url); }) : undefined}
          onCopyLink={configurationReady && audienceCompatible ? () => save(false) : undefined}>
          {audience === "none" ? <FormUnavailableNotice />
            : previewToken && service ? <PublicFormView key={previewToken} bounds={bounds} service={service} token={previewToken} inApp /> : preview}
        </FormPreviewDialog> : null}
        <FormHeadCover form={draft} pageId={pageId} workspaceId={productPages?.workspaceId ?? ""} onChange={changeDecor} />
        <FormBuilderColumn label={t("formView:editForm")} hasCover={Boolean(draft.cover)}>
          <FormHeadDecor form={draft} onChange={changeDecor} />
          {/* A long title runs on to more lines, as Notion's (seen 2026-11-06), staying one line of text. */}
          <textarea ref={titleRef} className={styles.formTitle} rows={1} value={draft.title} placeholder={t("formView:title")} aria-label={t("formView:title")}
            onChange={(event) => changeDraft((form) => ({ ...form, title: event.target.value.replace(/[\r\n]+/gu, " ") }))} onKeyDown={(event) => { if (fieldEnter(event)) event.preventDefault(); }} />
          <textarea className={styles.formDescription} rows={1} value={draft.description}
            placeholder={t("formView:descriptionOptional")} aria-label={t("formView:description")}
            onChange={(event) => changeDraft((form) => ({ ...form, description: event.target.value }))} />
          <FormAudienceNotice audience={audience} workspaceName={workspaceName} buttonRef={changeAudienceRef}
            onChange={() => { setShareAnchor("notice"); setShareOpen(true); }} />
          {/* While it asks, its title's question alone shows behind the card (Notion's, seen 2026-11-06). */}
          {renderNodes(startPending ? tree.filter(({ question }) => propertiesById.get(question.propertyId)?.type === "title") : tree, !startPending)}
          {/* Pressed, the "+" gives way to the new question's place, the menu beside it (Notion's, seen 2026-11-12). */}
          {addOpen ? <FormQuestionSlot ref={addSlotRef} /> : (
            <button ref={addButtonRef} type="button" className={styles.addQuestionButton} aria-label={t("formView:addQuestion")}
              title={t("formView:addQuestion")} aria-haspopup="menu" aria-expanded={addOpen}
              disabled={availableProperties.length === 0 && !onAddProperty}
              onClick={() => setAddOpen(true)} data-form-add-question>
              <Plus size="regular" aria-hidden="true" />
            </button>
          )}
          {addOpen ? (
            <FormAddQuestionMenu anchor={addSlotRef} beside properties={availableProperties} labels={availablePropertyLabels}
              canCreate={Boolean(onAddProperty)} onExisting={addExistingQuestion} onNew={(type) => void addNewQuestion(type)}
              onClose={() => setAddOpen(false)} />
          ) : null}
        </FormBuilderColumn>
      </div>
    </div>
  );
}

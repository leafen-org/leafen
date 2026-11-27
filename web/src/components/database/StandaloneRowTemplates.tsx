import { type ComponentProps, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GraphProperty } from "@/contentGraph";
import { confirmAction } from "@/components/ConfirmActionDialog";
import { deleteDatabaseRowPagePermanently } from "@/databaseRowPageApi";
import type {
  DatabaseToolbarConfigMetadataResponse,
  DatabaseToolbarConfigMutationInput,
  DatabaseToolbarTemplateMetadataWire,
} from "@/databaseToolbarConfigWire.generated";
import { mutateDatabaseToolbarConfig } from "@/databaseToolbarConfigApi";
import { createDatabaseToolbarConfigService } from "@/lib/databaseToolbarConfigService";
import {
  listenForDatabaseTemplateRequests, offerDatabaseTemplateActions, offerDatabaseTemplates,
  type DatabaseTemplateActions, type DatabaseTemplateChoice,
} from "@/lib/databaseTemplateRequests";
import { newId } from "@/lib/ids";
import { onTemplateDraftSent, useTemplateDraftsShown, withTemplateDrafts } from "@/lib/templateDraftSend";
import { holdTemplateUntilMade, openTemplateAtItsTitle, openTemplateCopy, shownTemplateCopy } from "@/lib/templatesMadeHere";
import type { Page, Session } from "@/types";
import { inlineDatabasePlaceholderTitle } from "../editor/databaseTitles";
import { DatabaseTemplateEditor, templateBandDatabaseIcon } from "./DatabaseTemplateEditor";
import { RowPageReading } from "./RowPageReading";
import type { DatabaseTemplatePropertyCells } from "./DatabaseTemplateProperties";
import { templateRepeats } from "./templateRepeatModel";
import { openTemplateWhole, templateOpenedWhole } from "./useTemplateAddress";

type Operation = DatabaseToolbarConfigMutationInput["operations"][number];

/**
 * The templates of a database page opened by its own address, where no view
 * of the database (whose toolbar keeps them) shows, as Notion's page has them
 * (seen 2026-11-01): its empty body lists them, and "템플릿을 생성하세요"
 * makes one and edits it in the window in the middle over the page — on a
 * touch screen as a whole page (DatabaseTemplateEditor). A template at its
 * own address (`whole`) opens whole here, as Notion's (seen 2026-11-25 in
 * Chrome at 1440, two tabs): drawn at once from the window that opened it
 * (templateOpenedWhole), its "복제" going to its copy's own address, whole,
 * and its "휴지통으로 이동" to its database (`onLeave`), each a step of its own.
 */
export function StandaloneRowTemplates({ session, pageId, dataSourceId, databaseTitle, databaseIcon, properties, propertyCells, workspaceId, onCommitProperties,
  whole, writable = true, pathDatabase, onLeave }: {
  session: Session;
  /** The page the database is on. */
  pageId: string;
  dataSourceId: string;
  databaseTitle: string;
  databaseIcon?: ComponentProps<typeof DatabaseTemplateEditor>["databaseIcon"];
  properties: readonly GraphProperty[];
  propertyCells: DatabaseTemplatePropertyCells;
  workspaceId: string;
  /** Saves its database's properties, as the page's own property editing does; none where they aren't the reader's to change. */
  onCommitProperties: ComponentProps<typeof DatabaseTemplateEditor>["onCommitProperties"];
  /** The template its own address opens whole (StandaloneRowPage). */
  whole?: string;
  /** Whether its templates are the reader's to change; one opened whole opens to read otherwise. */
  writable?: boolean;
  pathDatabase?: ComponentProps<typeof DatabaseTemplateEditor>["pathDatabase"];
  /** Leaves the template opened whole for its database, once it is gone. */
  onLeave?(): void;
}) {
  const { t } = useTranslation("databaseView");
  const [configuration, setConfiguration] = useState<DatabaseToolbarConfigMetadataResponse | null>(null);
  const service = useMemo(() => createDatabaseToolbarConfigService(session, setConfiguration), [session]);
  useEffect(() => {
    void service.refresh(pageId, dataSourceId).catch(() => undefined);
    return () => service.dispose();
  }, [dataSourceId, pageId, service]);
  // What a template's editor wrote shows at once, and the settings are read again once the server has it.
  const drafts = useTemplateDraftsShown(session, dataSourceId);
  // A template made here — new, or a copy — shows and opens at the press, in
  // its place, until the settings read holds it (templatesMadeHere.ts).
  // One opened whole from its window is drawn from what that held until the read holds it too.
  const [madeHere, setMadeHere] = useState<DatabaseToolbarTemplateMetadataWire[]>(() => {
    const held = whole ? templateOpenedWhole(whole) : null;
    return held ? [held] : [];
  });
  const templates = useMemo(() => {
    const read = configuration?.templates ?? [];
    const shown = [...read, ...madeHere.filter((made) => !read.some(({ id }) => id === made.id))];
    return withTemplateDrafts(shown.sort((left, right) => left.position < right.position ? -1 : left.position > right.position ? 1 : 0), drafts);
  }, [configuration, drafts, madeHere]);
  useEffect(() => onTemplateDraftSent(dataSourceId, () => void service.refresh(pageId, dataSourceId).catch(() => undefined)),
    [dataSourceId, pageId, service]);
  const [editing, setEditing] = useState<{ id: string; made: boolean } | null>(() => whole ? { id: whole, made: false } : null);
  const byId = (templateId: string) => templates.find((template) => template.id === templateId);

  // Whether it was kept; the settings are read again either way. One open whole sends its changes past its address — its
  // copy and its trash leave it at once — so leaving it gives none of them up, as leaving these settings does (dispose).
  const shown = useRef(true);
  useEffect(() => () => { shown.current = false; }, []);
  async function mutate(operations: Operation[]) {
    const current = service.snapshot();
    if (!current) return false;
    const input = { requestId: newId(), expectedWorkspaceContentRevision: current.workspaceContentRevision, operations };
    const kept = await (whole ? mutateDatabaseToolbarConfig(session, pageId, dataSourceId, input) : service.mutate(pageId, dataSourceId, input))
      .then((response) => Boolean(response), () => false);
    if (shown.current) await service.refresh(pageId, dataSourceId).catch(() => undefined);
    return kept;
  }
  const act = useRef({
    create: () => {},
    actions: null as DatabaseTemplateActions | null,
  });
  // Shown and opened at the press, its title taking what is typed next, the server making it behind; a phone's
  // copy opens with no caret (openTemplateCopy).
  const openMadeHere = (made: DatabaseToolbarTemplateMetadataWire, kept: Promise<boolean>, copy = false) => {
    const atTitle = copy ? openTemplateCopy(made.id, () => kept) : true;
    if (!copy) void openTemplateAtItsTitle(made.id, () => kept);
    setMadeHere((shown) => [...shown, made]);
    setEditing({ id: made.id, made: atTitle });
    void kept.then((ok) => {
      setMadeHere((shown) => shown.filter((template) => template.id !== made.id));
      if (!ok) setEditing((current) => current?.id === made.id ? null : current);
    });
  };
  act.current.create = () => {
    // Unnamed, as Notion makes it.
    const id = newId(), position = `${templates.at(-1)?.position ?? "m"}耀`;
    const kept = holdTemplateUntilMade(id, mutate([{ kind: "createTemplate", templateId: id, name: "", position, properties: {} }]), session.user.id);
    openMadeHere({ id, name: "", icon: "", title: "", properties: {}, isDefault: false, position,
      templateAuthorityRevision: 0, deleted: false, updatedAt: new Date().toISOString() }, kept);
  };
  // A copy, named and placed as the server makes it; its body, the server's copy, read once made.
  const duplicate = (templateId: string) => {
    const template = byId(templateId);
    if (!template) return;
    const id = newId(), copy = shownTemplateCopy(template, id, templates);
    const kept = holdTemplateUntilMade(id, mutate([{ kind: "duplicateTemplate", templateId, targetTemplateId: id, expectedRevision: template.templateAuthorityRevision }]));
    if (!whole) { openMadeHere(copy, kept, true); return; }
    // Whole, the copy opens whole at its own address as Notion's, a step of its own, its page read once made (templatesMadeHere.ts).
    openTemplateCopy(id, () => kept);
    openTemplateWhole(copy);
  };
  const remove = (templateId: string) => {
    const template = byId(templateId);
    if (template) void mutate([{ kind: "deleteTemplate", templateId, expectedRevision: template.templateAuthorityRevision }]);
  };
  act.current.actions = {
    template: (templateId) => templates.find((template) => template.id === templateId && !template.deleted),
    edit: (templateId) => setEditing({ id: templateId, made: false }),
    duplicate,
    remove: (templateId) => void confirmAction({ message: t("templateMenu.confirmDelete"), confirmLabel: t("templateMenu.delete"), danger: "filled" })
      .then((sure) => { if (sure) remove(templateId); }),
    setRepeat: (templateId, repeat) => {
      const template = byId(templateId);
      if (template) void mutate([{ kind: "setTemplateRepeat", templateId, expectedRevision: template.templateAuthorityRevision, repeat }]);
    },
  };
  // Offered by what they are, as the toolbar offers them.
  const choiceKey = JSON.stringify(templates.filter((template) => !template.deleted)
    .map((template) => ({ id: template.id, name: template.title.trim() || template.name, titled: Boolean(template.title.trim()), icon: template.icon,
      repeating: templateRepeats(template), isDefault: template.isDefault, hasContent: template.hasContent })));
  const choices = useMemo(() => JSON.parse(choiceKey) as DatabaseTemplateChoice[], [choiceKey]);
  useEffect(() => configuration ? offerDatabaseTemplates(dataSourceId, choices) : undefined, [choices, configuration, dataSourceId]);
  useEffect(() => listenForDatabaseTemplateRequests(dataSourceId, () => act.current.create()), [dataSourceId]);
  useEffect(() => offerDatabaseTemplateActions(dataSourceId, {
    template: (templateId) => act.current.actions?.template(templateId),
    edit: (templateId) => act.current.actions?.edit(templateId),
    duplicate: (templateId) => act.current.actions?.duplicate(templateId),
    remove: (templateId) => act.current.actions?.remove(templateId),
    setRepeat: (templateId, repeat) => act.current.actions?.setRepeat(templateId, repeat),
  }), [dataSourceId]);

  const template: DatabaseToolbarTemplateMetadataWire | undefined = editing ? byId(editing.id) : undefined;
  // Whole, it reads as its page does until its database's templates are read.
  if (!editing || !template) return whole ? <RowPageReading rowId={whole} /> : null;
  const close = () => { setEditing(null); if (whole) onLeave?.(); };
  return (
    <DatabaseTemplateEditor session={session} template={template} focusTitle={editing.made} whole={Boolean(whole)} pathDatabase={pathDatabase}
      databaseTitle={databaseTitle.trim() || inlineDatabasePlaceholderTitle()} databaseIcon={databaseIcon}
      properties={properties} propertyCells={propertyCells} writable={writable} workspaceId={workspaceId}
      pageId={pageId} dataSourceId={dataSourceId} onCommitProperties={onCommitProperties}
      onClose={close}
      onSetRepeat={(repeat) => act.current.actions?.setRepeat(template.id, repeat)}
      onDuplicate={() => duplicate(template.id)}
      onDelete={() => { remove(template.id); close(); }}
      onRestore={() => mutate([{ kind: "restoreTemplate", templateId: template.id, expectedRevision: template.templateAuthorityRevision }])}
      onDeleteForever={() => deleteDatabaseRowPagePermanently(session, template.id, {
        requestId: newId(), expectedAuthorityRevision: template.templateAuthorityRevision,
      }).then(() => service.refresh(pageId, dataSourceId)).then(() => true, () => false)} />
  );
}

/** The database a row's template band names (StandaloneRowPage): a full-page database's page, else the inline database as its row's path reads it (useRowPathDatabase), never the page holding it. */
export function templateBandDatabase(holder: Page | undefined, own: { title: string; icon: { icon: string; iconType: string } } | undefined, workspaceId: string) {
  const fullPage = holder?.kind === "database" ? holder : undefined;
  return { databaseTitle: (fullPage ? fullPage.title : own?.title) || inlineDatabasePlaceholderTitle(), databaseIcon: templateBandDatabaseIcon(fullPage, own?.icon, workspaceId) };
}

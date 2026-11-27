import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AdministratorWorkspacePage, AdministratorWorkspacePageContent } from "@/administratorWire.generated";
import { administratorWorkspaceContent } from "@/administratorWorkspaceContentApi";
import { ChevronDown, ChevronRight, GlobeIcon } from "@/icons/leafen";
import { newId } from "@/lib/ids";
import { userErrorText } from "@/lib/settingsErrors";
import { ReadFailed } from "../ReadFailed";
import type { Session } from "../../types";
import { confirmAction } from "../ConfirmActionDialog";
import styles from "./AdministratorSettingsPage.module.css";
import own from "./AdministratorWorkspaceContent.module.css";
import { ReadOnlyMarkdown } from "./ReadOnlyMarkdown";
import { listedPageNameKind, untitledPageDisplayTitle } from "../../lib/pageTitle";
import { titleWithClosedMentions } from "../../lib/closedMentions";

type Level = { items: AdministratorWorkspacePage[]; cursor: string | null };

const LEGAL_HOLD = "LEAFEN_LEGAL_HOLD_PREVENTS_PERMANENT_DELETE";

/**
 * A workspace's pages for a server administrator: its top pages by where they are, each
 * opened a level at a time, and the page chosen read beside them, to put in
 * the trash or, from the trash, delete.
 */
export function AdministratorWorkspacePages({ session, workspaceId, onChanged }: {
  session: Session; workspaceId: string;
  /** A page went to the trash or was deleted: the workspace's counts changed. */
  onChanged?: () => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [levels, setLevels] = useState<Record<string, Level>>({});
  const [open, setOpen] = useState<Record<string, boolean>>({});
  const [selected, setSelected] = useState("");
  // A failed read of a level, said with a way to read the same window again.
  const [error, setError] = useState<{ text: string; parentId: string; cursor?: string } | null>(null);

  async function load(parentId: string, cursor?: string) {
    setError(null);
    try {
      const page = await administratorWorkspaceContent.pages(session, workspaceId, { parentId: parentId || undefined, cursor });
      setLevels((current) => ({
        ...current,
        [parentId]: { items: cursor ? [...(current[parentId]?.items ?? []), ...page.items] : page.items, cursor: page.nextCursor },
      }));
    } catch (cause) {
      setError({ text: userErrorText(cause, t("workspaceContent.loadFailed")), parentId, cursor });
    }
  }

  useEffect(() => {
    void load("");
    // The pages load once for the workspace shown.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, workspaceId]);

  function toggle(page: AdministratorWorkspacePage) {
    const opening = !open[page.id];
    setOpen((current) => ({ ...current, [page.id]: opening }));
    if (opening && !levels[page.id]) void load(page.id);
  }

  // A page changed in the reader changes in its list too.
  function changed(pageId: string, change: "trashed" | "deleted") {
    setLevels((current) => Object.fromEntries(Object.entries(current).map(([parentId, level]) => [parentId, {
      ...level,
      items: change === "deleted"
        ? level.items.filter((item) => item.id !== pageId)
        : level.items.map((item) => item.id === pageId ? { ...item, inTrash: true } : item),
    }])));
    if (change === "deleted") setSelected("");
    onChanged?.();
  }

  function groupName(page: AdministratorWorkspacePage) {
    if (page.place === "teamspace") return page.placeName;
    if (page.place === "private") return t("workspaceContent.pages.private", { name: page.placeName || t("workspaceContent.unknownPerson") });
    return t("workspaceContent.pages.other");
  }

  function rows(parentId: string, depth: number): React.ReactNode {
    const level = levels[parentId];
    if (!level) return null;
    let lastGroup = "";
    return (
      <>
        {level.items.map((page) => {
          const group = depth === 0 ? `${page.place}\u0000${page.placeName}` : "";
          const header = depth === 0 && group !== lastGroup ? <div className={own.group}>{groupName(page)}</div> : null;
          lastGroup = group;
          return (
            <div key={page.id}>
              {header}
              <div className={own.row} data-selected={selected === page.id} style={{ paddingInlineStart: 4 + depth * 16 }}>
                {page.hasChildren ? (
                  <button type="button" className={own.toggle} aria-expanded={Boolean(open[page.id])}
                    aria-label={open[page.id] ? t("workspaceContent.pages.collapse") : t("workspaceContent.pages.expand")}
                    onClick={() => toggle(page)}>
                    {open[page.id] ? <ChevronDown size="small" aria-hidden="true" /> : <ChevronRight size="small" aria-hidden="true" />}
                  </button>
                ) : <span className={own.toggleSpacer} />}
                <button type="button" className={own.rowButton} onClick={() => setSelected(page.id)} data-admin-page={page.id}>
                  {page.iconType === "emoji" && page.icon ? <span aria-hidden="true">{page.icon}</span> : null}
                  <span className={own.rowTitle}>{titleWithClosedMentions(page.title, page.titleSpans) || untitledPageDisplayTitle(listedPageNameKind(page))}</span>
                  {page.isPublic ? <GlobeIcon size="small" aria-label={t("workspaceContent.pages.isPublic")} /> : null}
                  {page.inTrash ? <span className={own.tag}>{t("workspaceContent.pages.inTrash")}</span> : null}
                </button>
              </div>
              {open[page.id] ? rows(page.id, depth + 1) : null}
            </div>
          );
        })}
        {level.cursor ? (
          <button type="button" className={styles.secondaryButton} style={{ marginInlineStart: 4 + depth * 16 }}
            onClick={() => void load(parentId, level.cursor ?? undefined)}>{t("workspaceContent.more")}</button>
        ) : null}
      </>
    );
  }

  const top = levels[""];
  return (
    <div className={own.split}>
      <div className={own.tree}>
        {rows("", 0)}
        {top && !top.items.length ? <div className={own.hint}>{t("workspaceContent.pages.empty")}</div> : null}
        {error ? <ReadFailed message={error.text} onRetry={() => void load(error.parentId, error.cursor)} /> : null}
      </div>
      {selected ? (
        <AdministratorPageReader key={selected} session={session} workspaceId={workspaceId} pageId={selected} onChange={changed} />
      ) : <div className={own.hint}>{t("workspaceContent.pages.choose")}</div>}
    </div>
  );
}

function AdministratorPageReader({ session, workspaceId, pageId, onChange }: {
  session: Session;
  workspaceId: string;
  pageId: string;
  onChange: (pageId: string, change: "trashed" | "deleted") => void;
}) {
  const { t } = useTranslation("serverConsole");
  const [content, setContent] = useState<AdministratorWorkspacePageContent>();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [done, setDone] = useState("");
  const [deletion] = useState(() => newId());
  const [readTurn, setReadTurn] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    administratorWorkspaceContent.page(session, workspaceId, pageId, controller.signal).then(setContent, (cause) => {
      if (!controller.signal.aborted) setMessage(userErrorText(cause, t("workspaceContent.loadFailed")));
    });
    return () => controller.abort();
    // The page loads once for the page chosen, and again when asked.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [readTurn, session, workspaceId, pageId]);

  // Not read yet: what failed, with a way to read it again.
  if (!content) return <div className={own.reader}>{message ? <ReadFailed message={message} onRetry={() => { setMessage(""); setReadTurn((turn) => turn + 1); }} /> : null}</div>;
  const title = titleWithClosedMentions(content.page.title, content.page.titleSpans)
    || untitledPageDisplayTitle(listedPageNameKind(content.page));

  async function act(kind: "trash" | "delete" | "takeOver") {
    const confirmed = await confirmAction({
      message: t(`workspaceContent.pages.${kind}Message`, { title }),
      detail: t(`workspaceContent.pages.${kind}Detail`),
      confirmLabel: t(`workspaceContent.pages.${kind}`),
      danger: kind !== "takeOver",
    });
    if (!confirmed) return;
    setBusy(true);
    setMessage("");
    try {
      if (kind === "takeOver") {
        await administratorWorkspaceContent.takeOverPage(session, workspaceId, pageId);
        setContent((current) => current && { ...current, page: { ...current.page, takeOver: false } });
        setDone(t("workspaceContent.pages.takenOver"));
      } else if (kind === "trash") {
        await administratorWorkspaceContent.trashPage(session, workspaceId, pageId);
        setContent((current) => current && { ...current, page: { ...current.page, inTrash: true } });
        onChange(pageId, "trashed");
      } else {
        const result = await administratorWorkspaceContent.deletePage(session, workspaceId, pageId, deletion);
        if (result.status === "deleted") onChange(pageId, "deleted");
        else setMessage(t("workspaceContent.pages.deleting"));
      }
    } catch (cause) {
      const code = (cause as { code?: unknown } | null)?.code;
      setMessage(code === LEGAL_HOLD ? t("workspaceContent.pages.held") : userErrorText(cause, t("workspaceContent.failed")));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={own.reader} data-admin-reader={pageId}>
      <div className={own.readerHeader}>
        <h3 className={own.readerTitle}>{title}</h3>
        {content.page.isRow ? null : (
          <div className={own.readerActions}>
            {/* A private page whose person left, which no one may hold in full now. */}
            {content.page.takeOver && !content.page.inTrash ? (
              <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => void act("takeOver")}>
                {t("workspaceContent.pages.takeOver")}
              </button>
            ) : null}
            {content.page.inTrash ? (
              <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => void act("delete")}>
                {t("workspaceContent.pages.delete")}
              </button>
            ) : (
              <button type="button" className={styles.secondaryButton} disabled={busy} onClick={() => void act("trash")}>
                {t("workspaceContent.pages.trash")}
              </button>
            )}
          </div>
        )}
      </div>
      {content.page.isRow ? <div className={own.hint}>{t("workspaceContent.pages.rowHint")}</div> : null}
      {message ? <div className={own.error} role="alert">{message}</div> : null}
      {done ? <div className={own.hint} role="status">{done}</div> : null}
      <ReadOnlyMarkdown className={own.body} markdown={content.markdown} />
      {content.truncated ? <div className={own.hint}>{t("workspaceContent.pages.truncated")}</div> : null}
    </div>
  );
}

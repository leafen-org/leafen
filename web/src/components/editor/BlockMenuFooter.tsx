import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { EditedByLine, FootActivityPanel, type FootActivityRow, useFootActivityHover } from "@/components/RowMenuEditedFoot";
import { ownDigits } from "@/lib/i18n";
import { menuFootEditedTime } from "@/lib/menuFootWords";
import { readPageBlockLastEdit } from "@/lib/pageHistoryService";
import type { PageBlockLastEdit } from "@/pageHistoryWire.generated";
import type { Session } from "@/types";
import { blockTextCounts } from "./blockHandleModel";
import styles from "./editorStyles.generated.module.css";

/** Who the foot names for an edit or a making: an integration alone on its bot's face, an app's own account alone, else the person with an agent's connection beside them. */
type FootPerson = { id: string | null; name: string; via: string; bot?: boolean };

function footPerson(id: string | undefined, name: string | undefined, via: string | undefined, kind: string | undefined, app: boolean | undefined): FootPerson | null {
  // As Notion's block foot and its activity name a line an integration edited or made (seen 2026-11-26).
  if (via && kind === "integration") return { id: null, name: via, via: "", bot: true };
  return name ? { id: id ?? null, name, via: app ? "" : via ?? "", bot: app || undefined } : null;
}

const when = (at: number | undefined) => (at ? new Date(at).toISOString() : null);

/**
 * The foot of the block menu, as Notion's (seen 2026-10-01 and 2026-10-31):
 * "최종 편집 일시", who last edited the block with their face, when, then
 * the blocks' words and characters, in grey 12px lines under a divider that
 * take no clicks — the page menu's foot (RowMenuEditedFoot). Several blocks
 * chosen show their counts together and the last edit of the one `blockIds`
 * names, the block whose handle was pressed (seen 2026-11-26); a block
 * without text, as an image, shows no counts. Without `lastEdit` it shows
 * the counts alone. With `activity`, the pointer resting on it shows that
 * block's last editor and maker beside the menu (FootActivityPanel), as
 * Notion's block "⋮⋮" menu does (seen 2026-11-26 in Chrome 1440, two tabs:
 * a line an integration made and a person typed in named the person as its
 * editor and the integration as its maker, one no one touched the
 * integration twice).
 */
export function BlockMenuFooter({ session, pageId, blockIds, lastEdit = true, activity = false, texts }: {
  session: Session | null;
  pageId: string;
  blockIds: readonly string[];
  lastEdit?: boolean;
  activity?: boolean;
  /** The blocks' own text; none for a block that holds no text. */
  texts: readonly string[];
}) {
  const { t } = useTranslation(["blockHandle", "updatesPanel"]);
  const [edit, setEdit] = useState<PageBlockLastEdit | null>(null);
  const footRef = useRef<HTMLDivElement>(null);
  const ids = blockIds.join(",");
  useEffect(() => {
    if (!session || !ids || !lastEdit) return undefined;
    const controller = new AbortController();
    readPageBlockLastEdit(session, pageId, ids.split(","), controller.signal)
      .then(setEdit)
      .catch(() => {
        // The foot keeps its counts when the edit can't be read.
      });
    return () => controller.abort();
  }, [ids, lastEdit, pageId, session]);
  const counts = blockTextCounts(texts);
  // Its time and counts in the language's own digits, as Notion's foot writes
  // them (Arabic "اليوم الساعة ٩:٤٥ ص", "١٨ من الكلمات", seen 2026-11-19).
  const editedAt = edit?.editedAt && lastEdit ? menuFootEditedTime(edit.editedAt) : "";
  const editor = edit && lastEdit ? footPerson(edit.editorId, edit.editorName, edit.editorVia, edit.editorViaKind, edit.editorApp) : null;
  const maker = edit && lastEdit ? footPerson(edit.creatorId, edit.creatorName, edit.creatorVia, edit.creatorViaKind, edit.creatorApp) : null;
  const rows: FootActivityRow[] = activity && editor ? [
    { key: "editor", label: t("blockHandle:footer.activityEditor"), person: { id: editor.id, name: editor.name, at: when(edit?.editedAt) }, bot: editor.bot },
    ...(maker ? [{ key: "creator", label: t("blockHandle:footer.activityCreator"), person: { id: maker.id, name: maker.name, at: when(edit?.createdAt) }, bot: maker.bot }] : []),
  ] : [];
  const { open, pointed } = useFootActivityHover(rows.length > 0);
  return (
    <>
      <div ref={footRef} className={styles.blockMenuFoot} data-block-menu-footer
        onMouseEnter={rows.length ? () => pointed(true) : undefined} onMouseLeave={rows.length ? () => pointed(false) : undefined}>
        {editor || editedAt ? <span className={styles.blockMenuFootLabel}>{t("blockHandle:footer.lastEdited")}</span> : null}
        {editor ? <EditedByLine className={styles.blockMenuFootPerson} data={{ "data-block-menu-editor": "" }}
          userId={editor.id} name={editor.name} bot={editor.bot} via={editor.via} /> : null}
        {editedAt ? <span data-block-menu-edited-at>{editedAt}</span> : null}
        {texts.length > 0 ? <span data-block-menu-counts>{ownDigits(t("blockHandle:footer.counts", { words: counts.words, chars: counts.chars }))}</span> : null}
      </div>
      {open ? <FootActivityPanel foot={footRef} head={t("blockHandle:footer.activity")} rows={rows} pointed={pointed} /> : null}
    </>
  );
}

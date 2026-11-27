import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { CommentActorProfile } from "@/commentWire.generated";
import type { PublicContentDiscussionsResult } from "@/publicContentWire.generated";
import { CommentIcon } from "@/icons/leafen";
import type { NativeComment } from "@/lib/commentService";
import { PageDiscussionsPopover } from "@/components/comments/PageDiscussionsPopover";
import { pageDiscussions } from "./usePageCommentSummary";
import { styles } from "./topBarStyles";

type Discussions = ReturnType<typeof pageDiscussions> & { profiles: Record<string, CommentActorProfile> };

/**
 * A web link's visitor's "댓글" in the page's bar, as Notion's public page
 * (seen 2026-11-06): always there, it opens the page's "토론" to read —
 * the open discussions, or every one after "모두 보기". Its comments are read
 * when it opens, a window at a time to the newest, so a visitor sees them
 * all as they are then.
 */
export function PublicDiscussionsButton({ pageId, read }: {
  pageId: string;
  read: (pageId: string, cursor?: string, options?: { signal?: AbortSignal }) => Promise<PublicContentDiscussionsResult>;
}) {
  const { t } = useTranslation("topBar");
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [all, setAll] = useState(false);
  const [shown, setShown] = useState<Discussions | null>(null);

  function toggle() {
    if (open) { setOpen(false); return; }
    setAll(false);
    setOpen(true);
    void (async () => {
      const comments: NativeComment[] = [];
      const profiles: Record<string, CommentActorProfile> = {};
      let cursor = "";
      do {
        const result = await read(pageId, cursor);
        comments.push(...result.items.map((item) => ({
          id: item.id, pageId, workspaceId: "", blockId: item.blockId || null, parentId: item.parentId || null,
          authorId: item.authorId, body: item.body, resolved: item.resolved, revision: 0, reactions: [],
          createdAt: item.createdAt, updatedAt: item.createdAt, editedAt: item.editedAt || undefined,
        })));
        for (const person of result.people) profiles[person.userId] = person;
        cursor = result.nextCursor ?? "";
      } while (cursor);
      setShown({ ...pageDiscussions(comments), profiles });
    })().catch(() => {
      // Taken away meanwhile, or the network: the page's own check says so.
      setShown({ ...pageDiscussions([]), profiles: {} });
    });
  }

  return (
    <>
      <button ref={trigger} type="button" className={`${styles.action} ${styles.commentAction}`} data-public-discussions
        aria-haspopup="dialog" aria-expanded={open} title={t("discussionsTip")} aria-label={t("comment")} onClick={toggle}>
        <CommentIcon size="regular" aria-hidden="true" />
      </button>
      {open && shown ? (
        <PageDiscussionsPopover anchor={trigger} pageId={pageId} session={null} visitor showResolved={all}
          discussions={shown.discussions} latestReplies={shown.latestReplies} replies={shown.replies}
          actorProfiles={shown.profiles} onOpenAll={() => setAll(true)} onClose={() => setOpen(false)} />
      ) : null}
    </>
  );
}

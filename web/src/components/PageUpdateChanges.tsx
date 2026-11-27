import { FileText } from "@/icons/leafen";
import { useEffect, useState, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";

import { pageHref } from "@/lib/navigation";
import { pageUpdateItems, type PageUpdateDiff } from "@/lib/pageUpdates";
import type { PageUpdateProperty } from "@/lib/pageUpdateRecord";
import { blockAnchor } from "@/lib/blockAddress";
import { RichTextLine } from "./database/RichTextCellText";
import { PropertyIcon } from "./database/PropertyIcon";
import { PropertyTypeIcon } from "./database/PropertyTypeIcon";
import { updateLineBlank, updateLineBoxed } from "@/lib/updateLineKinds";
import { UpdateLine } from "./updates/UpdateLine";
import { UpdatePropertyValue } from "./updates/UpdatePropertyValue";
import { UPDATE_CHANGES_SHOWN, UpdateChangesMore } from "./updates/UpdateChangesMore";

import styles from "./PageAnalyticsPanel.module.css";

// The changes shown at a time once "N개 항목 더 보기" is pressed: the first
// at once, the rest over the next frames, so a long update opens at once.
const REVEAL_STEP = 50;

/**
 * What one of a page's updates changed, as Notion's updates tab lists it:
 * its lines and the title it gave in the order they were made — a database
 * page's record its properties and title (pageUpdateRecord.ts) — the first
 * five shown — the title one of them — and the rest under "N개 항목 더
 * 보기", which shows them in place (Notion, seen 2026-11-13).
 */
export function PageUpdateChanges({ diff, pageId, titleLabel, onFollow }: {
  diff: PageUpdateDiff;
  pageId: string;
  /** What names a title change: a database row's title property's name. */
  titleLabel?: string;
  onFollow(event: MouseEvent<HTMLAnchorElement>): void;
}) {
  const { t } = useTranslation("pageAnalytics");
  const { t: tUpdates } = useTranslation("updatesPanel");
  const items = pageUpdateItems(diff);
  const [limit, setLimit] = useState<number>(UPDATE_CHANGES_SHOWN);
  const open = limit > UPDATE_CHANGES_SHOWN;
  // Once open, the rest follow a frame at a time.
  useEffect(() => {
    if (!open || limit >= items.length) return;
    const frame = requestAnimationFrame(() => setLimit((current) => current + REVEAL_STEP));
    return () => cancelAnimationFrame(frame);
  }, [open, limit, items.length]);
  const folded = open ? 0 : items.length - Math.min(items.length, limit);

  // A changed line, linked to its block in the page, or to the page where it
  // was deleted, each deleted one under its own "삭제됨" (Notion's, seen
  // 2026-11-13, two lines deleted at once two labels); a sub-page its page.
  // Each draws by its block's kind (UpdateLine), with no step in for a line
  // inside a toggle or a list, as the workspace's updates draw it (Notion's,
  // seen 2026-11-17 by workers w1645 and w1658 in two tabs).
  const lineLink = (line: PageUpdateDiff["lines"][number]) => {
    const href = line.page && line.change === "added" ? pageHref(line.blockId)
      : line.change === "deleted" ? pageHref(pageId) : pageHref(pageId) + blockAnchor(pageId, line.blockId);
    const link = (
      <a key={line.blockId} className={styles.updateLine} data-change={line.change} data-block-type={line.blockType}
        data-boxed={!line.page && updateLineBoxed(line.blockType, line.change) ? "" : undefined}
        data-blank={line.change === "added" && updateLineBlank(line.blockType) ? "" : undefined}
        data-page-line={line.page ? "true" : undefined} href={href} onClick={onFollow}>
        {line.page ? <><FileText size="regular" aria-hidden="true" /><span>{line.added}</span></>
        : <UpdateLine change={line.change} blockType={line.blockType} checked={line.checked} language={line.language} was={line.was}
          steps={line.steps} text={line.before + line.added + line.after}>
          <RichTextLine text={line.before} spans={line.spans?.before} />
          {line.change === "changed"
            ? <span data-added><RichTextLine text={line.added} spans={line.spans?.added} /></span>
            : <RichTextLine text={line.added} spans={line.spans?.added} />}
          <RichTextLine text={line.after} spans={line.spans?.after} />
        </UpdateLine>}
      </a>
    );
    return line.change === "deleted" ? (
      <div key={line.blockId} className={styles.updateDeleted}>
        <span className={styles.updateDeletedLabel}>{tUpdates("inbox.activity.deleted")}</span>{link}
      </div>
    ) : link;
  };

  // A property's icon and name over its value as its cell shows it, a text's
  // added words marked, a cleared one's old value struck (Notion's, seen 2026-11-13).
  const propertyValue = ({ property, before, after, words, spans, cleared }: PageUpdateProperty) => (
    <div key={`property:${property.id}`} className={styles.updateTitle} data-page-update-property={property.id}>
      <span className={styles.updateTitleLabel}>
        <PropertyIcon property={property} />
        <span>{property.name}</span>
      </span>
      <a href={pageHref(pageId)} onClick={onFollow} data-change={cleared ? "cleared" : undefined}>
        <UpdatePropertyValue property={property} value={cleared ? before : after} was={cleared ? undefined : before} words={words} spans={spans} />
      </a>
    </div>
  );

  return (
    <div className={styles.updateChanges} data-page-update-changes={items.length}>
      {items.slice(0, limit).map((item) => "line" in item ? lineLink(item.line) : "property" in item ? propertyValue(item.property) : (
        // The title property's icon and name over the new title, its added
        // words marked, as Notion's (seen 2026-11-06).
        <div key="title" className={styles.updateTitle}>
          <span className={styles.updateTitleLabel}>
            <PropertyTypeIcon type="title" />
            <span>{titleLabel?.trim() || t("updates.titleLabel")}</span>
          </span>
          <a href={pageHref(pageId)} onClick={onFollow}>
            {diff.titleParts ? <>{diff.titleParts.before}
              {diff.titleParts.added ? <span data-added>{diff.titleParts.added}</span> : null}
              {diff.titleParts.after}</> : diff.title}
          </a>
        </div>
      ))}
      {folded > 0 ? <UpdateChangesMore count={folded} onOpen={() => setLimit(UPDATE_CHANGES_SHOWN + REVEAL_STEP)} /> : null}
    </div>
  );
}

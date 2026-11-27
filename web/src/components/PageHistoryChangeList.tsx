"use client";

import { titleWithClosedMentions } from "@/lib/closedMentions";
import type { TextSpan } from "@/lib/types";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { useCallback, useEffect, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { ArrowLeft, ChevronRight, EyeSlashIcon } from "@/icons/leafen";
import {
  appendPageHistoryChanges, pageHistoryChangeKey, pageHistoryDateText,
} from "@/lib/pageHistoryChangeCards";
import { KOREAN_OBJECT_PAIR, KOREAN_SUBJECT_PAIR } from "@/lib/koreanParticles";
import type { PageHistoryAuthor, PageHistoryGroup } from "@/lib/pageHistoryGroups";
import type { PageHistoryService } from "@/lib/pageHistoryService";
import { pagePresenceColor } from "@/lib/pagePresenceModel";
import type { PageHistoryActorProfile, PageHistoryChange } from "@/pageHistoryWire.generated";
import { pageHistoryRowAction } from "./PageHistoryBookmarks";
import styles from "./PageHistoryChangeList.module.css";

type ChangeView = {
  group: PageHistoryGroup;
  changes: PageHistoryChange[];
  cursor: string | null;
  loading: boolean;
  selectedKey: string;
};

/**
 * An entry's changes as the history reads them: opening an entry reads its
 * first page of changes and chooses the newest, as Notion's does.
 */
export function usePageHistoryChanges(
  service: PageHistoryService,
  onProfiles: (profiles: PageHistoryActorProfile[]) => void,
  onError: (caught: unknown) => void,
) {
  const [view, setView] = useState<ChangeView | null>(null);
  const viewRef = useRef(view);
  viewRef.current = view;
  const read = useCallback((group: PageHistoryGroup, cursor?: string) => {
    const sinceId = group.versionIds.at(-1) ?? group.version.id;
    service.readPageHistoryChanges(group.version.id, sinceId, cursor).then((result) => {
      if (!result || viewRef.current?.group.version.id !== group.version.id) return;
      onProfiles(result.actorProfiles);
      setView((current) => {
        if (!current || current.group.version.id !== group.version.id) return current;
        const changes = cursor ? appendPageHistoryChanges(current.changes, result.changes) : appendPageHistoryChanges([], result.changes);
        return {
          ...current, changes, cursor: result.hasMore ? result.nextCursor : null, loading: false,
          selectedKey: current.selectedKey || (changes[0] ? pageHistoryChangeKey(changes[0]) : ""),
        };
      });
    }).catch((caught) => {
      setView((current) => (current ? { ...current, loading: false } : current));
      onError(caught);
    });
  }, [onError, onProfiles, service]);

  useEffect(() => setView(null), [service]);

  return {
    view,
    selected: view?.changes.find((change) => pageHistoryChangeKey(change) === view.selectedKey) ?? null,
    open(group: PageHistoryGroup) {
      setView({ group, changes: [], cursor: null, loading: true, selectedKey: "" });
      read(group);
    },
    close: () => setView(null),
    select: (change: PageHistoryChange) => setView((current) => (
      current ? { ...current, selectedKey: pageHistoryChangeKey(change) } : current
    )),
    loadMore() {
      if (!view?.cursor || view.loading) return;
      setView({ ...view, loading: true });
      read(view.group, view.cursor);
    },
  };
}

/**
 * An entry's changes one by one, as Notion's "버전의 모든 변경 사항 보기"
 * (seen 2026-10-01): "← 모든 버전", the entry's card, then a card for each
 * change, newest first, with its author's initial, what they did and the
 * time to the second, the cards joined by a line down their initials.
 */
export function PageHistoryChangeList({
  titleId, view, onSelect, onBack, onLoadMore, actorLabel, timeLabel, changeTimeLabel,
}: {
  titleId: string;
  view: ChangeView;
  onSelect: (change: PageHistoryChange) => void;
  onBack: () => void;
  onLoadMore: () => void;
  actorLabel: (actorId: string) => string;
  timeLabel: (createdAt: number) => string;
  changeTimeLabel: (createdAt: number) => string;
}) {
  const { t, i18n } = useTranslation(["updatesPanel", "blocks", "wikiView", "databaseView"]);
  const blockName = (type: string) => {
    if (type === "paragraph") return t("updatesPanel:versions.changes.textBlock");
    if (type === "inline_database" || type === "child_database") return t("updatesPanel:versions.changes.databaseBlock");
    return type ? t(`blocks:defs.${type}.label`, { defaultValue: "" }) : "";
  };
  const typeName = (type: string) => t(`blocks:defs.${type}.label`, { defaultValue: type });
  // A row value as its line reads it: "—" for none, a date in the reader's
  // words, a mention closed to the reader as "@찾을 수 없음" (closedMentions.ts).
  // An open one's spans, kept with its words, say it "@" and its name, as Notion's lists.
  const rowValueText = (type: string, text: string, spans?: TextSpan[]) => !text ? "—"
    : type === "date" ? text.split(" → ").map((part) => pageHistoryDateText(part, i18n.language)).join(" → ") : titleWithClosedMentions(text, spans);
  const sentence = (change: PageHistoryChange) => {
    const name = actorLabel(change.actorId);
    const values = { name, subject: KOREAN_SUBJECT_PAIR, type: blockName(change.blockType), view: change.viewName?.trim() || (change.viewType ? t(`databaseView:viewTypes.${change.viewType}`, { defaultValue: change.viewType }) : "") };
    const key = change.kind === "edited" ? (values.type ? "edited" : "editedBlock") : change.kind;
    return (
      <Trans t={t} i18nKey={`updatesPanel:versions.changes.${key}`} values={values}
        components={{ name: <span className={styles.changeName} /> }} />
    );
  };
  const { group } = view;
  return (
    <section className={styles.changePane} aria-label={t("updatesPanel:versions.versionHistory")}>
      <span id={titleId} className={styles.srOnly}>{t("updatesPanel:versions.versionHistory")}</span>
      <button type="button" className={styles.back} onClick={onBack}>
        <ArrowLeft size="small" aria-hidden="true" />{t("updatesPanel:versions.allVersions")}
      </button>
      <div className={styles.entryCard}>
        <span className={styles.entryTime}>{timeLabel(group.version.createdAt)}</span>
        {group.authors.length > 0 ? (
          <span className={styles.entryActors}>
            {group.authors.map((author, index) => (
              <span key={`${author.actorId}:${author.connectionName}`} className={styles.entryActor}>
                <span className={styles.entryActorDot} style={{ background: pagePresenceColor(author.actorId) }} aria-hidden="true" />
                <PageHistoryAuthorName author={author} actorLabel={actorLabel} />{index < group.authors.length - 1 ? "," : ""}
              </span>
            ))}
          </span>
        ) : null}
      </div>
      <div className={`${styles.changeList} nscroll`} role="listbox" aria-label={t("updatesPanel:versions.changesAria")}>
        {view.changes.map((change) => {
          const key = pageHistoryChangeKey(change);
          const selected = key === view.selectedKey;
          return (
            <button key={key} type="button" role="option" aria-selected={selected} className={styles.changeCard}
              data-selected={selected ? "true" : undefined} data-page-history-change={change.kind}
              onClick={() => onSelect(change)}>
              <PersonAvatar className={styles.changeAvatar} userId={change.actorId} name={actorLabel(change.actorId)} size={16} />
              <span className={styles.changeBody}>
                <span className={styles.changeSentence}>{sentence(change)}</span>
                {change.kind === "typeChanged" ? (
                  <span className={styles.changeDetail}>
                    {t("updatesPanel:versions.changes.typeDetail", { from: typeName(change.fromType), to: typeName(change.blockType) })}
                  </span>
                ) : null}
                {/* As Notion's: a renamed property's old and new names, a new view's kind. */}
                {change.kind === "propertyChanged" && change.fromName !== undefined && change.toName !== undefined ? (
                  <span className={styles.changeDetail}>
                    {t("updatesPanel:versions.changes.typeDetail", { from: change.fromName, to: change.toName })}
                  </span>
                ) : null}
                {change.kind === "viewCreated" && change.viewType ? (
                  <span className={styles.changeDetail}>{t(`databaseView:viewTypes.${change.viewType}`, { defaultValue: change.viewType })}</span>
                ) : null}
                {/* As Notion's: an edited row's changed values, a line each ("Stage: A → B"). */}
                {change.kind === "rowEdited" ? change.rowValues?.map((value) => (
                  <span key={value.propertyId} className={styles.changeDetail} data-page-history-row-value={value.propertyId}>
                    {t("updatesPanel:versions.changes.rowValue", {
                      property: value.propertyName, from: rowValueText(value.type, value.from, value.fromSpans), to: rowValueText(value.type, value.to, value.toSpans),
                    })}
                  </span>
                )) : null}
                <span className={styles.changeTime}>{changeTimeLabel(change.createdAt)}</span>
              </span>
            </button>
          );
        })}
        {!view.loading && view.changes.length === 0 ? (
          <div className={styles.empty}>{t("updatesPanel:versions.noChanges")}</div>
        ) : null}
        {view.cursor ? (
          <button type="button" className={styles.loadMore} disabled={view.loading} onClick={onLoadMore}>
            {t("wikiView:loadMore")}
          </button>
        ) : null}
      </div>
    </section>
  );
}

/** Over a preview that can't show the chosen change, as Notion's: "시각화할 수 없음". */
export function PageHistoryChangeUnshown() {
  const { t } = useTranslation("updatesPanel");
  return (
    <div className={styles.unshown} role="status" data-page-history-change-unshown>
      <EyeSlashIcon size="regular" aria-hidden="true" />
      <strong className={styles.unshownTitle}>{t("versions.cannotVisualize")}</strong>
      <span className={styles.unshownBody}>{t("versions.cannotVisualizeBody")}</span>
    </div>
  );
}

/** The entry's "버전의 모든 변경 사항 보기" in the version list, at its first line's right end. */
/**
 * Who made an entry's changes: the person, and the connection they made them
 * through, as Notion's history names it ("〈이름〉이(가) ChatGPT를 통해", seen
 * 2026-10-01).
 */
export function PageHistoryAuthorName({ author, actorLabel }: {
  author: PageHistoryAuthor;
  actorLabel: (actorId: string) => string;
}) {
  const { t } = useTranslation();
  const name = actorLabel(author.actorId);
  if (!author.connectionName) return <>{name}</>;
  // An integration works as its own bot: Notion's history names it alone (seen 2026-11-05).
  if (author.integration) return <>{author.connectionName}</>;
  return (
    <>
      {t("updatesPanel:versions.byConnection", {
        name, subject: KOREAN_SUBJECT_PAIR,
        connection: author.connectionName, object: KOREAN_OBJECT_PAIR,
      })}
    </>
  );
}

export function PageHistoryShowChangesButton({ onShow }: { onShow: () => void }) {
  const { t } = useTranslation("updatesPanel");
  return (
    <button type="button" {...pageHistoryRowAction} data-shape="round" aria-label={t("versions.showChanges")}
      title={t("versions.showChanges")} data-page-history-show-changes onClick={onShow}>
      <ChevronRight size="tiny" aria-hidden="true" />
    </button>
  );
}

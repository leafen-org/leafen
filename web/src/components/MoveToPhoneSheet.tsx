import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { ArrowDown, TableIcon } from "@/icons/leafen";
import { PhoneSheet, PhoneSheetGroup, PhoneSheetRow } from "@/components/shell/PhoneSheet";
import { PhoneSheetNoResults } from "@/components/shell/PhoneSheetNoResults";
import { MovePhoneSearch } from "./MovePhoneSearch";
import type { Place } from "@/lib/pagePath";
import { moveGroupOf, type MoveDestination, type MoveGroup } from "./pageMoveDialogModel";
import { PageIconGlyph } from "./page/PageIcon";
import { PlaceLine } from "./PlaceLine";
import { PersonAvatar } from "@/components/people/PersonAvatar";
import { MoveTeamspaceGlyph } from "./MoveDestinationRow";
import { isMoveListDatabase, rankedAmongPages, type MoveListDatabaseRow } from "./moveSearchOrder";

/**
 * "옮기기" on a phone, as Notion's (seen 2026-10-31, 375px): a whole sheet
 * headed "옮기기" with "완료", the search under it, and under "추천",
 * "팀스페이스" and, once searched, "페이지" (MoveGroup) a row for each place,
 * a page's with where it is in gray, the databases found among the pages
 * (rankedAmongPages); the workspace at the foot. A search that finds nothing
 * shows "결과 없음" alone in a group's row (seen 2026-11-13, 402pt).
 */
export function MoveToPhoneSheet({ query, onQuery, placeholder, notice, destinations, databases, nothingFound, foot, owner, onChoose, onChooseDatabase, onMore, onClose }: {
  /** The reader, whose mark the private pages show. */
  owner: { id: string; name: string };
  query: string;
  onQuery: (query: string) => void;
  placeholder: string;
  /** What another workspace's list says first (that the page is copied there). */
  notice?: ReactNode;
  destinations: readonly MoveDestination[];
  databases: readonly { id: string; title: string; score: number; location: Place }[];
  /** Nothing to go to, the search answered. */
  nothingFound?: boolean;
  foot?: ReactNode;
  onChoose: (destination: MoveDestination) => void;
  onChooseDatabase: (database: { id: string; title: string }) => void;
  /** Shows a search's next answers, "더 불러오기" under them as the window's (moveSearchWindow.tsx); a phone's own row not yet seen in Notion. */
  onMore?: () => void;
  onClose: () => void;
}) {
  const { t } = useTranslation(["moveToDialog", "rowMenu", "common"]);
  const searching = Boolean(query.trim());
  type Found = (typeof databases)[number];
  const groups: { group: MoveGroup; destinations: (MoveDestination | MoveListDatabaseRow<Found>)[] }[] = [];
  const listed = rankedAmongPages(destinations, (destination) => destination.type === "page" && searching
    ? { score: destination.score, label: destination.label } : null, databases);
  for (const destination of listed) {
    const group = isMoveListDatabase<Found>(destination) ? destination.group : moveGroupOf(destination, searching);
    if (groups[groups.length - 1]?.group === group) groups[groups.length - 1].destinations.push(destination);
    else groups.push({ group, destinations: [destination] });
  }
  return (
    <PhoneSheet title={t("rowMenu:actions.moveTo")} label={t("moveToDialog:title")} onClose={onClose}
      action={{ label: t("common:actions.done"), onPress: onClose }} foot={foot} data={{ "data-move-popover": "phone" }}>
      <MovePhoneSearch value={query} label={t("moveToDialog:search.label")} placeholder={placeholder} onValueChange={onQuery} />
      {notice}
      {groups.map(({ group, destinations: inGroup }) => (
        <PhoneSheetGroup key={group} label={t(`moveToDialog:results.${group}`)}>
          {inGroup.map((destination) => isMoveListDatabase<Found>(destination) ? (
            <PhoneSheetRow key={destination.key} more={false} icon={<TableIcon size="regular" />}
              label={destination.database.title} description={destination.database.location.length ? <PlaceLine place={destination.database.location} /> : undefined}
              onPress={() => onChooseDatabase(destination.database)} data={{ "data-move-database": destination.database.id }} />
          ) : (
            <PhoneSheetRow key={`${destination.type}:${destination.id}`} more={false} disabled={destination.disabled}
              icon={destination.type === "page" ? <PageIconGlyph page={destination.page} size={20} />
                : destination.place.teamspaceId ? <MoveTeamspaceGlyph place={destination.place} />
                  : <PersonAvatar userId={owner.id} name={owner.name} size={20} />}
              label={destination.label} description={destination.type === "page" && destination.where.length ? <PlaceLine place={destination.where} /> : undefined}
              onPress={() => onChoose(destination)} data={{ "data-move-destination": destination.id }} />
          ))}
          {onMore && group === groups[groups.length - 1].group ? <PhoneSheetRow more={false} muted icon={<ArrowDown size="regular" />}
            label={t("moveToDialog:results.loadMore")} onPress={onMore} data={{ "data-move-load-more": "" }} /> : null}
        </PhoneSheetGroup>
      ))}
      {nothingFound ? <PhoneSheetNoResults>{t("moveToDialog:results.empty")}</PhoneSheetNoResults> : null}
    </PhoneSheet>
  );
}

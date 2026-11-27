import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy, DotsHorizontal, Plus, ResetIcon, SaveForEveryoneIcon, Trash } from "@/icons/leafen";
import type { DbProperty, ViewSort } from "@/lib/types";
import { PhoneSheetGroup, PhoneSheetRow } from "../shell/PhoneSheet";
import { PhoneChoiceSheet, PhonePropertyMenu, PhonePropertyScreen } from "./DatabasePhonePickers";
import { PropertyIcon } from "./PropertyIcon";
import { databaseViewLabels } from "./databaseViewLabels";
import { propertySortLabels } from "./tablePropertyMenuModel";
import { sortableProperty } from "./query";
import styles from "./DatabasePhoneSortList.module.css";

/** The viewer's own sorts against the view's: saving them for everyone or as a new view, or going back to the view's. */
export type PhoneSortSave = { onReset(): void; onSaveForEveryone?(): void; onSaveAsNewView?(): void };

/**
 * A phone's sorts, as Notion's "정렬" screen (seen 2026-10-21 at 375px): each
 * sort a white group of its property ("Aa Name ›", whose pick is a screen of
 * properties), its direction ("A → Z로 정렬 ›", whose two choices rise in a
 * half sheet that closes once one is picked) and a red "삭제"; then grey
 * "+ 정렬 추가", whose unsorted properties rise from the bottom under a
 * search — those a view can sort by, as its first "새 정렬" lists them
 * (Notion's, seen 2026-11-12: no date or as-they-are rollup) —, and "정렬 제거". With none, "+ 정렬 추가" alone. Their order is
 * the order they were added in, without handles, as Notion's. While the
 * viewer's own sorts differ from the view's, a last group (seen 2026-10-24)
 * of orange "모두에게 저장", "정렬 초기화" and "추가 설정", which opens it to
 * a grey note that the view's sorts were changed over the first two and "새
 * 보기로 저장".
 */
export function DatabasePhoneSortList({ sorts, properties, save, onCommit }: {
  sorts: ViewSort[];
  properties: DbProperty[];
  save?: PhoneSortSave | null;
  onCommit(sorts: ViewSort[]): void;
}) {
  const labels = databaseViewLabels();
  const { t } = useTranslation("tableView");
  const [direction, setDirection] = useState<number | null>(null);
  const [picking, setPicking] = useState<number | null>(null);
  const [adding, setAdding] = useState(false);
  const [saveOpen, setSaveOpen] = useState(false);
  const used = new Set(sorts.map(({ propertyId }) => propertyId));
  const change = (index: number, patch: Partial<ViewSort>) => onCommit(sorts.map((sort, current) => current === index ? { ...sort, ...patch } : sort));
  const directionLabels = (sort: ViewSort) => {
    const [ascending, descending] = propertySortLabels(properties.find(({ id }) => id === sort.propertyId), properties);
    return { asc: t(ascending), desc: t(descending) };
  };
  const directing = direction === null ? undefined : sorts[direction];
  const repicking = picking === null ? undefined : sorts[picking];
  return (
    <>
      {sorts.map((sort, index) => {
        const property = properties.find(({ id }) => id === sort.propertyId);
        return (
          <PhoneSheetGroup key={sort.propertyId}>
            <PhoneSheetRow icon={property ? <PropertyIcon property={property} size="regular" /> : undefined} indent label={property?.name ?? ""}
              onPress={() => setPicking(index)} data={{ "data-sort-setting": sort.propertyId }} />
            <PhoneSheetRow indent label={directionLabels(sort)[sort.direction]} onPress={() => setDirection(index)}
              data={{ "data-sort-direction": sort.direction }} />
            <PhoneSheetRow icon={<Trash size="regular" />} label={labels.sheetDelete} danger more={false}
              onPress={() => onCommit(sorts.filter((_sort, current) => current !== index))} data={{ "data-sort-delete": sort.propertyId }} />
          </PhoneSheetGroup>
        );
      })}
      <PhoneSheetGroup>
        <PhoneSheetRow icon={<Plus size="regular" />} label={labels.addSort} muted more={false} onPress={() => setAdding(true)} data={{ "data-add-sort": "" }} />
        {sorts.length > 0 ? (
          <PhoneSheetRow icon={<Trash size="regular" />} label={labels.removeSorts} muted more={false} onPress={() => onCommit([])}
            data={{ "data-remove-sorts": "" }} />
        ) : null}
      </PhoneSheetGroup>
      {save ? (
        <PhoneSheetGroup className={styles.save}>
          {saveOpen ? <p className={styles.note}>{labels.sortsChangedFromView}</p> : null}
          {save.onSaveForEveryone ? (
            <PhoneSheetRow icon={<SaveForEveryoneIcon size="regular" />} label={labels.saveViewForEveryone} more={false}
              onPress={save.onSaveForEveryone} data={{ "data-save-view-for-everyone": "" }} />
          ) : null}
          <PhoneSheetRow icon={<ResetIcon size="regular" />} label={labels.resetOwnSorts} muted more={false} onPress={save.onReset}
            data={{ "data-own-sorts-reset": "" }} />
          {save.onSaveAsNewView && saveOpen ? (
            <PhoneSheetRow icon={<Copy size="regular" />} label={labels.saveAsNewView} muted more={false} onPress={save.onSaveAsNewView}
              data={{ "data-save-as-new-view": "" }} />
          ) : save.onSaveAsNewView ? (
            <PhoneSheetRow icon={<DotsHorizontal size="regular" />} label={labels.moreSettings} muted more={false} onPress={() => setSaveOpen(true)}
              data={{ "data-own-sorts-more": "" }} />
          ) : null}
        </PhoneSheetGroup>
      ) : null}
      {directing && direction !== null ? (
        <PhoneChoiceSheet title={labels.sorts} data={{ "data-sort-directions": "" }} onClose={() => setDirection(null)}
          choices={(["asc", "desc"] as const).map((key) => ({ key, label: directionLabels(directing)[key] }))}
          onPick={(key) => change(direction, { direction: key })} />
      ) : null}
      {repicking && picking !== null ? (
        <PhonePropertyScreen properties={properties.filter((property) => property.id === repicking.propertyId || (!used.has(property.id) && sortableProperty(property)))}
          onClose={() => setPicking(null)}
          onPick={(property) => { change(picking, { propertyId: property.id }); setPicking(null); }} />
      ) : null}
      {adding ? (
        <PhonePropertyMenu label={labels.sheetAddSort} properties={properties.filter((property) => !used.has(property.id) && sortableProperty(property))}
          onClose={() => setAdding(false)}
          onPick={(property) => { onCommit([...sorts, { propertyId: property.id, direction: "asc" }]); setAdding(false); }} />
      ) : null}
    </>
  );
}

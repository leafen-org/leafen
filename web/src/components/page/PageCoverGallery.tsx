import { useRef } from "react";
import { useTranslation } from "react-i18next";
import { COVER_GALLERY, type CoverGalleryGroup } from "@/lib/covers";
import { NativeChoiceGrid } from "@/lib/nativeChoiceGrid";
import { coverImage } from "./pagePresentation";
import styles from "./PageCover.module.css";

function GalleryGroup({ group, currentCover, busy, onApplyCover }: {
  group: CoverGalleryGroup; currentCover: string; busy: boolean; onApplyCover(cover: string): void;
}) {
  const { t } = useTranslation("pageCover");
  const listRef = useRef<HTMLDivElement>(null);
  return <section className={styles.galleryGroup}>
    <h3>{t(`gallery.${group.id}`)}</h3>
    <NativeChoiceGrid items={group.covers} keyOf={item => item.value}
      textOf={item => t(`gallery.${item.label}`)} checkedKey={currentCover}
      disabledKeys={busy ? group.covers.map(item => item.value) : []}
      listRef={listRef} label={t(`gallery.${group.id}`)} className={styles.galleryGrid}
      itemClassName={styles.coverTile} autoFocus={false}
      marker={() => ({ "data-cover-tile": "true", "data-cover-menu-item": "true" })}
      onAction={item => onApplyCover(item.value)}>
      {item => <span className={styles.coverTilePaint} style={{ backgroundImage: coverImage(item.value) }} aria-hidden="true" />}
    </NativeChoiceGrid>
  </section>;
}

export function PageCoverGallery(props: { currentCover: string; busy: boolean; onApplyCover(cover: string): void }) {
  return <div className={styles.gallery}>{COVER_GALLERY.map(group => <GalleryGroup key={group.id} group={group} {...props} />)}</div>;
}

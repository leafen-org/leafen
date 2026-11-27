import type { HTMLAttributes } from "react";
import { MiddleTitle } from "../MiddleTitle";
import { PlaceLine } from "../PlaceLine";
import { VerificationBadge } from "../VerificationBadge";
import { isPageVerified } from "@/lib/pageVerification";
import type { MentionItem } from "./blockPickerModel";
import { PickerArchivedMark } from "./PickerArchivedPage";
import styles from "./editorStyles.generated.module.css";

/**
 * A page row's words in the "@", "[[" and "+" menus (MentionMenu): its
 * title on its line — cut at its end, or in its middle once words are
 * searched (Notion's, seen 2026-11-12) — with a verified page's mark after
 * it and an archived page's at the line's end, and where it sits on a second
 * line, in its name (rowNameProps).
 */
export function MentionPageText({ item, searched, labelProps, placeProps, descriptionProps }: {
  item: Extract<MentionItem, { kind: "page" }>;
  searched: boolean;
  labelProps: HTMLAttributes<HTMLElement>;
  placeProps: HTMLAttributes<HTMLElement>;
  descriptionProps: HTMLAttributes<HTMLElement>;
}) {
  return (
    <span className={styles.slashText}>
      <span className={styles.slashName} {...labelProps}>
        {searched ? <MiddleTitle title={item.label} after={isPageVerified(item.page) ? <VerificationBadge page={item.page} compact /> : null} />
          : <><span className={styles.slashNameText}>{item.label}</span><VerificationBadge page={item.page} compact /></>}
        {item.archived ? <PickerArchivedMark place="end" /> : null}
      </span>
      {item.place?.length ? <PlaceLine className={styles.slashDesc} {...placeProps} place={item.place} />
        : item.description ? <span className={styles.slashDesc} {...descriptionProps}>{item.description}</span> : null}
    </span>
  );
}

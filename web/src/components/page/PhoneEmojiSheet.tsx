import { useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { PhoneSheet } from "@/components/shell/PhoneSheet";
import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { EmojiCatalogPanel } from "./EmojiCatalogPanel";
import styles from "./PhoneEmojiSheet.module.css";

/**
 * A reaction's emoji picker on a phone, as Notion's (seen 2026-11-14 at
 * 411px in Android Chrome, its iPhone's alike): the whole screen, "리액션"
 * over its line with a blue "닫기", a white "필터" band and ✋, the groups'
 * names on the sheet's ground over white rows of 32px emoji in 44px cells,
 * and the groups' bar at its foot. It takes the focus as it opens, so the
 * keyboard goes down and nothing gets the caret back when it closes; the
 * filter waits for a tap. The workspace's own emoji are offered without
 * adding one, the bar's "사용자 지정" pressable even with none.
 */
export function PhoneEmojiSheet({ marker, onClose, onPick, custom }: {
  marker?: string;
  onClose: () => void;
  onPick: (emoji: string) => void;
  custom?: { workspaceId: string; onPick: (emoji: WorkspaceCustomEmoji) => void };
}) {
  const { t } = useTranslation(["commentsPanel", "common"]);
  const [query, setQuery] = useState("");
  const sheetRef = useRef<HTMLDivElement>(null);
  // Before the sheet notes what had the focus, so what opened it (a line's
  // words) doesn't take the caret back as it closes.
  useLayoutEffect(() => { sheetRef.current?.focus({ preventScroll: true }); }, []);
  return (
    <PhoneSheet size="screen" title={t("commentsPanel:reactions.sheetTitle")} sheetRef={sheetRef} onClose={onClose}
      action={{ label: t("common:actions.close"), onPress: onClose }} bodyClassName={styles.body}
      data={{ "data-emoji-sheet": "true", ...(marker ? { [`data-${marker}`]: "true" } : {}) }}>
      <EmojiCatalogPanel offerRandom={false} search={{ query, onQuery: setQuery, waits: true }} workspaceId={custom?.workspaceId}
        onPick={onPick} onClose={onClose}
        custom={custom ? { workspaceId: custom.workspaceId, refreshKey: 0, onPick: custom.onPick, offeredEmpty: true } : undefined} />
    </PhoneSheet>
  );
}

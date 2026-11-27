import { useOpenedFromKeyboard } from "@/lib/nativeMenu";
import {
  type RefObject,
  useEffect,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import type { WorkspaceCustomEmoji } from "@/lib/customEmoji";
import { createFocusManager } from "@react-aria/focus";
import { FocusScope, useDialog } from "react-aria";
import { Tabs, TabList, Tab, TabPanel } from "react-aria-components";
import { useOverlayLayer } from "@/lib/overlayLayer";
import { useFitPopoverInWindow } from "@/lib/popoverPlacement";
import type { WorkspaceFileTarget } from "@/lib/storage";
import { CustomEmojiAddDialog } from "./CustomEmojiAddDialog";
import { EmojiCatalogPanel } from "./EmojiCatalogPanel";
import { PageIconBuiltInPanel } from "./PageIconPickerPanels";
import { PageIconUploadPanel } from "./PageIconUploadPanel";
import { useBuiltInIconChoice } from "./builtInIconChoice";
import { TouchPickerHead, touchPickerCard, usePhonePickerCard, useTouchPickerCard } from "@/components/shell/TouchPickerCard";
import { hasLifecycleTrackedIconUploadTarget, openingIconTab, PAGE_ICON_TABS, type IconPickerTab, type PickerCurrentIcon } from "./pageIconPickerModel";
import styles from "./PageIconPicker.module.css";


export function PageIconPicker({
  onPick,
  onPickImage,
  onPickCustomEmoji,
  onPickBuiltInIcon,
  onRemove,
  onClose,
  uploadTarget,
  placement = "page",
  label,
  offerRandom = true,
  onShuffle,
  pickAddedEmoji = true,
  onLayerZIndexChange,
  returnFocusRef,
  returnFocusFromKeysOnly = false,
  leavesFocus = false,
  suggestedEmojis,
  short = false,
  uploadsImage = true,
  pageIcon = false,
  tabOrder = PAGE_ICON_TABS,
  current,
  openingTab,
  iconColors = true,
}: {
  /** Whether its icons tab offers their colors (its ● button); a property's picker has none, its icons gray (builtInIconChoice). */
  iconColors?: boolean;
  /** Its tabs' order, the picker opening on the first it offers (ICON_FIRST_TABS for a teamspace), or on "아이콘" for a built-in icon. */
  tabOrder?: readonly IconPickerTab[];
  /** The icon it is opened on, which picks the tab it opens on (openingIconTab). */
  current?: PickerCurrentIcon;
  /** The tab it opens on where its opener names one, past that rule: a tab group's opens on its icon's kind (DesktopStripMenus.tsx). */
  openingTab?: IconPickerTab;
  /**
   * A page's own icon: on a touch screen wider than a phone its picker opens
   * as Notion's iPad card, "제거 · 페이지 아이콘 · 닫기" (TouchPickerCard),
   * its "제거" in the card's head rather than the tab row.
   */
  pageIcon?: boolean;
  /** Whether a picked image may be uploaded as the icon: not where nothing keeps the file (a callout's, until its block holds its icon's file). */
  uploadsImage?: boolean;
  /** Emoji offered in a row of their own after the recent ones (a callout's "콜아웃"). */
  suggestedEmojis?: readonly string[];
  /** 356px high, as Notion's picker for an icon not there yet (a callout's from "아이콘 편집"). */
  short?: boolean;
  /** Picks an emoji; without it the picker offers no emoji tab, as a property's icon picker in Notion. */
  onPick?: (emoji: string) => void;
  onPickImage?: (url: string) => void;
  onPickCustomEmoji?: (emoji: WorkspaceCustomEmoji) => void;
  onPickBuiltInIcon?: (value: string) => void;
  onRemove?: () => void;
  onClose: () => void;
  uploadTarget?: WorkspaceFileTarget;
  /** Where it opens: below a page's icon, below an inline icon, or at the top left of its own layer. */
  placement?: "page" | "inline" | "anchored";
  /** The dialog's name when it picks something other than an icon. */
  label?: string;
  /** Whether the emoji tab offers a random emoji, as Notion's icon picker does. */
  offerRandom?: boolean;
  /**
   * Puts in the random button's emoji or built-in icon without it joining the
   * recent ones; the picker then stays open at a desk and closes on a touch
   * card. Without it the random one is picked as any.
   */
  onShuffle?: (value: string, iconType: "emoji" | "icon") => void;
  /** Whether an emoji added here is picked at once (an icon) or only joins the list (a reaction), as Notion's. */
  pickAddedEmoji?: boolean;
  onLayerZIndexChange?: (value: number | undefined) => void;
  returnFocusRef?: RefObject<HTMLElement | null>;
  /** On the page, a picker opened with the mouse leaves no focus behind, as Notion's page icon (seen 2026-11-04),
   *  in a window too (data-focus-leaves: its removal is not made up for by the window's own keeper). */
  returnFocusFromKeysOnly?: boolean;
  /** Opened by a press that gives the caret to a field opened with it (a resting margin card's reaction, which opens
   *  the card with the caret in its reply box): it takes no focus — its filter waits for a press, it holds none in —
   *  and gives none back as it closes, as Notion's (CommentReactions useReactionPicker). */
  leavesFocus?: boolean;
}) {
  const { t } = useTranslation("emojiPicker");
  const offered: Record<IconPickerTab, boolean> = { emoji: Boolean(onPick), icons: Boolean(onPickBuiltInIcon), custom: Boolean(onPickImage) };
  const tabs = tabOrder.filter(key => offered[key]);
  const [tab, setTab] = useState<IconPickerTab>(() => openingTab && tabs.includes(openingTab) ? openingTab : openingIconTab(tabs, current));
  const [query, setQuery] = useState("");
  const [catalogRefreshKey, setCatalogRefreshKey] = useState(0);
  // The "+" or "추가" the add dialog hangs from, while it is open.
  const [addAnchor, setAddAnchor] = useState<HTMLElement | null>(null);
  const pickerRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const touchCard = useTouchPickerCard() && pageIcon;
  // A random pick leaves the picker open at a desk for another, as Notion's
  // (seen 2026-11-17 in Chrome at 1440, two tabs: either tab's random, the
  // icon changed and the picker stayed); a touch card closes after it as
  // after any pick, as Notion's iPad (seen 2026-11-17 in Safari at 834 and
  // 1210, two tabs ten minutes apart: either tab's random, an icon or an
  // emoji tapped, each time the card closed with the icon in).
  const shuffle = onShuffle && ((value: string, iconType: "emoji" | "icon") => {
    onShuffle(value, iconType);
    if (touchCard) onClose();
  });
  const iconChoice = useBuiltInIconChoice((value) => onPickBuiltInIcon?.(value), iconColors, shuffle && ((value) => shuffle(value, "icon")));
  const phoneCard = usePhonePickerCard() && touchCard;
  useFitPopoverInWindow(pickerRef, placement !== "anchored" && !touchCard);
  // One tab alone (an emoji picker only) hides the tab row; its panel then
  // offers "제거" itself, else the row does at its right, as Notion's.
  const tabsHidden = tabs.length === 1 && Boolean(onPick);
  const panelRemove = tabsHidden ? onRemove : undefined;
  const backdropRef = useRef<HTMLButtonElement>(null);
  const keysNow = useOpenedFromKeyboard();
  const [openedFromKeys] = useState(keysNow);
  const layer = useOverlayLayer(true, { kind: "picker", floats: false, ref: pickerRef, onEscape: onClose, onOutsidePress: onClose,
    restoreFocus: !leavesFocus && (!returnFocusFromKeysOnly || openedFromKeys), returnFocusRef });
  // Opened without taking the focus (leavesFocus), it gets none from React Aria's dialog either: neither its focus as it
  // mounts nor the second one half a second later, which took the picker whenever the focus had gone to the page by
  // then — an Escape from the reply box within 0.5 s left the next key to the picker, where Notion's lands nowhere
  // (2026-11-26, Chrome 1440, two tabs). `isEntering` is the hook's own hold on both.
  const { dialogProps } = useDialog({ "aria-label": label ?? t("dialog.label"), isEntering: leavesFocus }, pickerRef);
  useEffect(() => { onLayerZIndexChange?.(layer.zIndex); }, [layer.zIndex, onLayerZIndexChange]);
  // A touch card takes the focus itself, its filter waiting for a tap with the keyboard down, as Notion's iPad.
  // Measured 2026-11-16 after the press only (no script in the page before it), Notion's iPad Pro 11 Safari (two tabs
  // 11 minutes apart): "아이콘 추가" and the icon tapped, no caret in "필터", the keyboard down (Leafen the same).
  useEffect(() => { if (touchCard) pickerRef.current?.focus({ preventScroll: true }); }, [touchCard]);
  const canUploadImageFile = hasLifecycleTrackedIconUploadTarget(uploadTarget);

  function selectTab(nextTab: IconPickerTab) {
    iconChoice.cancel();
    setTab(nextTab);
  }

  function pickCustomEmoji(emoji: WorkspaceCustomEmoji) {
    if (onPickCustomEmoji) onPickCustomEmoji(emoji);
    else onPickImage?.(emoji.fileId);
  }

  // A new workspace emoji is the icon at once, as Notion's (seen 2026-11-01):
  // a notice that the emoji joined the workspace, and the picker closes. A
  // reaction's picker stays open with it in "사용자 지정", reacting with nothing.
  function added() {
    // The window says it was added (CustomEmojiAddDialog).
    setCatalogRefreshKey((current) => current + 1);
  }

  return (
    <>
      <button
        ref={backdropRef}
        type="button"
        className={touchCard ? `${styles.backdrop} ${touchPickerCard.shade}` : styles.backdrop}
        style={{ zIndex: layer.zIndex === undefined ? undefined : layer.zIndex - 1 }}
        tabIndex={-1}
        aria-label={t("dialog.backdropLabel")}
      />
      <div
        ref={pickerRef}
        className={touchCard ? `${styles.picker} ${touchPickerCard.card}` : styles.picker}
        data-placement={placement}
        data-focus-leaves={returnFocusFromKeysOnly && !openedFromKeys ? "" : undefined}
        data-touch-card={touchCard ? "true" : undefined}
        tabIndex={touchCard ? -1 : undefined}
        data-icon-tabs-hidden={tabsHidden ? "true" : undefined}
        data-icon-picker-short={short ? "true" : undefined}
        {...dialogProps}
        style={{ zIndex: layer.zIndex }}
        onKeyDown={layer.onKeyDown}
      >
        <FocusScope contain={!leavesFocus}>
        {touchCard ? <TouchPickerHead title={t("pageIconCard.title")} removeLabel={t("remove.label")} onRemove={onRemove} onClose={onClose} /> : null}
        <Tabs selectedKey={tab} onSelectionChange={key => selectTab(key as IconPickerTab)}>
          <div className={touchCard ? `${styles.head} ${touchPickerCard.tabRow}` : styles.head} style={tabsHidden ? { display: "none" } : undefined}>
            <TabList className={styles.tabs} aria-label={t("tabs.groupLabel")}>
              {tabs.map(key => <Tab key={key} id={key} className={styles.tab} data-icon-tab={key}
                onPress={event => {
                  if (event.pointerType !== "keyboard") createFocusManager(panelRef).focusFirst({ tabbable: true });
                }}>
                {t(`tabs.${key === "custom" ? "image" : key}`)}
              </Tab>)}
            </TabList>
            {onRemove && !touchCard ? (
              <button type="button" className={styles.headRemove} data-icon-remove onClick={onRemove}>
                {t("remove.label")}
              </button>
            ) : null}
          </div>
          <TabPanel key={tab} id={tab} ref={panelRef}>
        {tab === "icons" ? (
          <PageIconBuiltInPanel choice={iconChoice} query={query} onQuery={setQuery} onRemove={panelRemove} onClose={onClose} touchCard={Boolean(touchCard)} />
        ) : tab === "emoji" && onPick ? (
          <>
            <EmojiCatalogPanel offerRandom={offerRandom} onShuffle={shuffle && ((emoji) => shuffle(emoji, "emoji"))} search={{ query, onQuery: setQuery, waits: touchCard || leavesFocus }} workspaceId={uploadTarget?.workspaceId} suggested={suggestedEmojis} onPick={onPick} onRemove={panelRemove} onClose={onClose}
              custom={uploadTarget?.workspaceId && canUploadImageFile && (onPickCustomEmoji || onPickImage) ? {
                workspaceId: uploadTarget.workspaceId,
                refreshKey: catalogRefreshKey,
                onPick: pickCustomEmoji,
                onAdd: phoneCard ? undefined : setAddAnchor,
                offeredEmpty: phoneCard,
              } : undefined} />
            {addAnchor && uploadTarget ? (
              <CustomEmojiAddDialog anchor={addAnchor} uploadTarget={uploadTarget} onClose={() => setAddAnchor(null)}
                onAdded={(emoji) => {
                  setAddAnchor(null);
                  added();
                  if (pickAddedEmoji) pickCustomEmoji(emoji);
                }} />
            ) : null}
          </>
        ) : onPickImage ? (
          <PageIconUploadPanel uploadTarget={uploadTarget} canUpload={canUploadImageFile && uploadsImage} onPickImage={onPickImage}
            onPickCustomEmoji={onPickCustomEmoji} onAddedToLibrary={added} onCancel={onClose} />
        ) : null}
          </TabPanel>
        </Tabs>
        </FocusScope>
      </div>
    </>
  );
}

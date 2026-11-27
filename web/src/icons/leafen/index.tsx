import {
  ArrowBendUpLeft as PhosphorArrowBendUpLeft,
  ArrowsDownUp as PhosphorArrowsDownUp,
  PaperPlaneTilt as PhosphorPaperPlaneTilt,
  ArrowClockwise as PhosphorArrowClockwise,
  ArrowCounterClockwise as PhosphorArrowCounterClockwise,
  ArrowElbowDownLeft as PhosphorArrowElbowDownLeft,
  ArrowUUpLeft as PhosphorArrowUUpLeft,
  Bell as PhosphorBell,
  BellSlash as PhosphorBellSlash,
  Books as PhosphorBooks,
  CaretDoubleLeft as PhosphorCaretDoubleLeft,
  CaretDoubleRight as PhosphorCaretDoubleRight,
  CaretDown as PhosphorCaretDown,
  CaretLeft as PhosphorCaretLeft,
  CaretRight as PhosphorCaretRight,
  CaretUp as PhosphorCaretUp,
  CheckCircle as PhosphorCheckCircle,
  ArrowElbowDownRight as PhosphorArrowElbowDownRight,
  PersonSimpleRun as PhosphorPersonSimpleRun,
  TreeView as PhosphorTreeView,
  Clock as PhosphorClock,
  Infinity as PhosphorInfinity,
  Alarm as PhosphorAlarm,
  CornersOut as PhosphorCornersOut,
  DownloadSimple as PhosphorDownloadSimple,
  EnvelopeSimple as PhosphorEnvelopeSimple,
  EnvelopeSimpleOpen as PhosphorEnvelopeSimpleOpen,
  Eye as PhosphorEye,
  EyeSlash as PhosphorEyeSlash,
  Globe as PhosphorGlobe,
  House as PhosphorHouse,
  List as PhosphorList,
  LockSimple as PhosphorLockSimple,
  LockSimpleOpen as PhosphorLockSimpleOpen,
  Pause as PhosphorPause,
  Play as PhosphorPlay,
  ArrowsInSimple as PhosphorArrowsInSimple,
  ArrowsOutSimple as PhosphorArrowsOutSimple,
  PlusSquare as PhosphorPlusSquare,
  Repeat as PhosphorRepeat,
  SpeakerHigh as PhosphorSpeakerHigh,
  Waveform as PhosphorWaveform,
  ArrowsClockwise as PhosphorArrowsClockwise,
  FileArrowUp as PhosphorFileArrowUp,
  Star as PhosphorStar,
  Heart as PhosphorHeart,
  Tray as PhosphorTray,
  TrayArrowDown as PhosphorTrayArrowDown,
  UploadSimple as PhosphorUploadSimple,
  CloudArrowUp as PhosphorCloudArrowUp,
  CloudSlash as PhosphorCloudSlash,
  Users as PhosphorUsers,
  Warning as PhosphorWarning,
  Browser as PhosphorBrowser,
  BuildingOffice as PhosphorBuildingOffice,
  PlayCircle as PhosphorPlayCircle,
  X as PhosphorX,
  Info as PhosphorInfo,
  Paperclip as PhosphorPaperclip,
  XCircle as PhosphorXCircle,
  StackPlus as PhosphorStackPlus,
  NotePencil as PhosphorNotePencil,
  CirclesFour as PhosphorCirclesFour,
  Archive as PhosphorArchive,
  ArrowUpRight as PhosphorArrowUpRight,
  ArrowUpLeft as PhosphorArrowUpLeft,
  Password as PhosphorPassword,
  ShieldCheck as PhosphorShieldCheck,
} from "@phosphor-icons/react";
import { ICON_SIZES, IconSvg, PhosphorIconSvg, smallMark, type LeafenIconProps } from "./iconPrimitives";
import { ToggleCaretIcon } from "./textMarkIcons";
import { LockFillIcon } from "./notionMarkIcons";
import { ViewEyeIcon, ViewEyeSlashIcon } from "./viewIcons";

export type { GlyphFrame, LeafenIconProps, LeafenIconSize } from "./iconPrimitives";
export { ICON_SIZES, iconAtSize, iconPixels } from "./iconPrimitives";
export * from "./emojiGroupIcons";
export * from "./databaseIcons";
export * from "./pageMenuIcons";
export * from "./blockIcons";
export * from "./serviceIcons";
export * from "./libraryIcons";
export * from "./inboxIcons";
export * from "./mediaIcons";
export * from "./viewIcons";
export * from "./serverSettingsIcons";
export * from "./textMarkIcons";
export * from "./propertyIcons";
export * from "./phoneToolIcons";
export * from "./commentIcons";
export * from "./menuActionIcons";
export * from "./pageActionMarks";
export * from "./pageGlyphIcons";
export * from "./chartSettingIcons";

export const ChevronRight = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCaretRight} />
);

/**
 * A row's or a group's disclosure, Notion's caret (ToggleCaretIcon) drawn as
 * Notion sizes it: 0.8em of the words it stands by (seen 2026-11-14 — a
 * library row's and a sub-item's 12.8 by 16px, a group's and a teamspace
 * row's 11.2 by 14px, the sub-items picture's 9.6 by 12px), so a screen sets
 * that size, not the icon's. A named size draws it in that box instead.
 */
export const CaretRightFill = (p: LeafenIconProps) => (
  <ToggleCaretIcon {...(p.size ? {} : { width: "0.8em", height: "0.8em" })} {...p} />
);

export const PlayIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPlay} />
);

export const PauseIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPause} />
);

export const ChevronLeft = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCaretLeft} />
);

export const ArrowLeft = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrow-left">
    <path d="M9.2 5.2 4.4 10l4.8 4.8" />
    <path d="M5 10h10.6" />
  </IconSvg>
);

export const ArrowRight = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrow-right">
    <path d="m10.8 5.2 4.8 4.8-4.8 4.8" />
    <path d="M4.4 10H15" />
  </IconSvg>
);

// A text's direction, left to right or right to left: the screen's own
// sides, so these do not turn over with the reading direction as the arrows
// above do (styles.css).
export const LeftToRightIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="left-to-right">
    <path d="m10.8 5.2 4.8 4.8-4.8 4.8" />
    <path d="M4.4 10H15" />
  </IconSvg>
);

export const RightToLeftIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="right-to-left">
    <path d="M9.2 5.2 4.4 10l4.8 4.8" />
    <path d="M5 10h10.6" />
  </IconSvg>
);

// Notion's "자동 감지" text direction: arrows both ways.
export const ArrowsLeftRight = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrows-left-right">
    <path d="M4.4 7.2h11.2" />
    <path d="m12.6 4.2 3 3-3 3" />
    <path d="M15.6 12.8H4.4" />
    <path d="m7.4 9.8-3 3 3 3" />
  </IconSvg>
);

// Notion's two-way relation: an arrow out each way from a gap in the middle (seen 2026-11-07).
export const ArrowsExpandHorizontal = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrows-expand-horizontal">
    <path d="M8.9 10H2.8" />
    <path d="m5.6 7.2-2.8 2.8 2.8 2.8" />
    <path d="M11.1 10h6.1" />
    <path d="m14.4 7.2 2.8 2.8-2.8 2.8" />
  </IconSvg>
);

// Notion's dashboard "행으로 이동" (seen 2026-11-03): arrows up and down.
export const ArrowsDownUpIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowsDownUp} />
);

export const ArrowUp = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrow-up">
    <path d="M5.2 9.2 10 4.4l4.8 4.8" />
    <path d="M10 5v10.6" />
  </IconSvg>
);

export const ArrowDown = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="arrow-down">
    <path d="m5.2 10.8 4.8 4.8 4.8-4.8" />
    <path d="M10 4.4V15" />
  </IconSvg>
);

export const ChevronDown = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCaretDown} />
);

export const ChevronUp = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCaretUp} />
);

export const DoubleChevronLeft = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCaretDoubleLeft} />
);

export const DoubleChevronRight = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCaretDoubleRight} />
);

// A published site's "추가 작업" on a phone: three lines, as Notion's (seen 2026-10-19).
export const HamburgerIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorList} />
);

/**
 * A chosen row's ✓, drawn after Notion's one tick (checkmarkSmall, seen
 * 2026-11-15 in the share window's role menu, a block's "전환" and a phone's
 * view and choice sheets): a short arm down to the corner and a long one up,
 * filling 10 of 16 square, its line 1.25 of 16 at every size Notion draws it
 * (16px in a menu, 18 in a phone sheet). Notion's checkbox draws the same
 * tick.
 */
export const CheckIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="check" line={1.25}>
    <path d="M5.63 10.29 9.13 14.37 14.37 5.63" />
  </IconSvg>
);

export const CheckCircleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCheckCircle} />
);

// A database's "추가 설정" marks, in the shapes of Notion's (seen 2026-11-03):
// nested rows for its sub-items, a turning arrow for its dependencies and a
// runner for its sprints.
export const SubitemsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTreeView} />
);
export const DependencyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowElbowDownRight} />
);
export const SprintIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPersonSimpleRun} />
);

/** A page with a plus, as Notion's file preview marks "페이지에 추가" (seen 2026-11-06). */
export const AddToPageIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPlusSquare} />
);

export const X = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorX} crop={1.3} />
);

/**
 * A "⋯", Notion's three dots: their middles 0.3 of the size apart, each as
 * large as Notion's at that size — its 16px mark's (ellipsisSmall, 2.55 across
 * at 16 and 12.15 in all, seen 2026-11-15 in a phone's "N개 보기" and a
 * sidebar row) below 18px, its 20px glyph's (ellipsis, 2.75 across at 20, the
 * top bar's) from there (smallMark).
 */
export const DotsHorizontal = (p: LeafenIconProps) => {
  const r = smallMark(p.pixels ?? ICON_SIZES[p.size ?? "small"]) ? 1.275 : 1.1;
  return (
    <IconSvg {...p} glyph="dots-three" fill="currentColor" stroke="none">
      <circle cx="5.2" cy="10" r={r} />
      <circle cx="10" cy="10" r={r} />
      <circle cx="14.8" cy="10" r={r} />
    </IconSvg>
  );
};

// Filled, as a property list's eye; a menu row may ask for the outline, Notion's eye (ViewEyeIcon).
export const EyeIcon = (p: LeafenIconProps) => (
  p.weight === "regular" ? <ViewEyeIcon {...p} /> : <PhosphorIconSvg weight="fill" {...p} icon={PhosphorEye} />
);

/** The eye drawn in line, as the other rows of a menu are (a page menu's "열람 기록"). */
export const EyeLineIcon = ViewEyeIcon;

export const EyeSlashIcon = (p: LeafenIconProps) => (
  p.weight === "regular" ? <ViewEyeSlashIcon {...p} /> : <PhosphorIconSvg weight="fill" {...p} icon={PhosphorEyeSlash} />
);

export const Home = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorHouse} />
);

export const Bell = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBell} />
);

export const BellSlashIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBellSlash} />
);

export const EnvelopeIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorEnvelopeSimple} />
);

export const EnvelopeOpenIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorEnvelopeSimpleOpen} />
);

export const MailIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTray} />
);

// A tray taking an arrow down, as Notion's "모두에게 저장" (seen 2026-10-24).
export const SaveForEveryoneIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTrayArrowDown} />
);

// An archive box, as Notion's archive marks and actions (seen 2026-10-07).
export const ArchiveIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArchive} />
);

export const Download = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorDownloadSimple} />
);

export const PaperclipIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPaperclip} />
);

export const FullscreenIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCornersOut} />
);


export const LogOutIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="log-out">
    <path d="M8.4 4.6H5.7c-.9 0-1.6.7-1.6 1.6v7.6c0 .9.7 1.6 1.6 1.6h2.7" />
    <path d="M9.2 10h6.4" />
    <path d="m12.7 6.9 3.1 3.1-3.1 3.1" />
  </IconSvg>
);

// A database page's peek modes: beside the view, over its center, and the
// full page. Notion's side peek mark (seen 2026-11-08, its 20px drawing in a
// table row's "열기" and the row menu's "사이드 보기"): the center peek's
// frame with a filled box down its right side.
export const SidePeekIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="side-peek">
    <rect x="4.4" y="5.8" width="11.2" height="8.4" rx="1.2" />
    <rect x="9.9" y="6.9" width="4.56" height="6.2" rx="0.4" fill="currentColor" stroke="none" />
  </IconSvg>
);

// A database page's "세부 정보 보기" in its top bar, as Notion's (seen
// 2026-11-11): a frame with its right side parted off, the details panel's.
export const PageDetailsIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="page-details">
    <rect x="4" y="4.8" width="12" height="10.4" rx="1.6" />
    <path d="M11.8 4.8v10.4" />
  </IconSvg>
);

// Notion's center peek mark (seen 2026-11-07, its 20px drawing; the box's
// width 2026-11-13, 5.43–14.58 of 20): a wide frame with a filled box in its middle.
export const CenterPeekIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="center-peek">
    <rect x="4.4" y="5.8" width="11.2" height="8.4" rx="1.2" />
    <rect x="6.344" y="7.7" width="7.32" height="4.6" rx="0.4" fill="currentColor" stroke="none" />
  </IconSvg>
);

// Notion's quick find highlight pane toggle (seen 2026-11-07, its 20px
// drawing): a frame with a pane down its right side.
export const HighlightPaneIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="highlight-pane">
    <rect x="3.8" y="5.4" width="12.4" height="9.2" rx="1.2" />
    <path d="M12.4 5.4v9.2" />
  </IconSvg>
);

// Notion's full page peek mark (seen 2026-11-13, its 20px drawing in a
// peek's "보기 옵션 변경"): the peeks' frame filled nearly whole.
export const FullPagePeekIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="full-page-peek">
    <rect x="4.4" y="5.8" width="11.2" height="8.4" rx="1.2" />
    <rect x="5.544" y="6.904" width="8.92" height="6.2" rx="0.4" fill="currentColor" stroke="none" />
  </IconSvg>
);

// Notion's new tab mark (seen 2026-11-13, its 20px drawing in a peek's
// "보기 옵션 변경" › "새로운 탭"): a window with tabs along its top, the
// first one open into it.
export const NewTabIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="new-tab">
    <rect x="5" y="5" width="10" height="10" rx="1.6" />
    <path d="M8.8 5v2h6.2" />
    <path d="M11.7 5v2" />
  </IconSvg>
);

export const SmileIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="smile">
    <circle cx="10" cy="10" r="6.2" />
    <path d="M7.4 8h.01" />
    <path d="M12.6 8h.01" />
    <path d="M7.5 11.6c1.1 1.3 3.9 1.3 5 0" />
  </IconSvg>
);

/** A picture wider than high, a filled sun over two peaks, the right one taller, as
 * Notion's (a phone's caret tools, seen 2026-11-13). */
export const ImageIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="image">
    <rect x="3.87" y="5.47" width="12.27" height="9.07" rx="1.07" />
    <circle cx="8.27" cy="8" r=".88" fill="currentColor" />
    <path d="m3.87 11.87 2-2 2.4 2.4 4.27-4.27 3.6 3.6" />
  </IconSvg>
);

export const GlobeIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorGlobe} />
);

export const LibraryIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBooks} />
);

export const LockIcon = (p: LeafenIconProps) => (p.weight === "fill" ? <LockFillIcon {...p} />
  : <PhosphorIconSvg {...p} icon={PhosphorLockSimple} />);

export const UnlockIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorLockSimpleOpen} />
);

export const SharePeopleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUsers} />
);

// The share button of a page everyone in its workspace opens, as Notion's (seen 2026-11-08): an office building.
export const ShareWorkspaceIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBuildingOffice} />
);

// The share button of a page published as a site, as Notion's (seen 2026-11-05): a browser window.
export const SharePublishedIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBrowser} />
);

export const Upload = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUploadSimple} />
);

// Notion's import drop place (seen 2026-10-30): a cloud with an arrow up.
export const CloudUploadIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCloudArrowUp} />
);

// Notion's mark for no connection beside the workspace's name (seen 2026-11-14): a struck-out cloud.
export const CloudOffIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCloudSlash} />
);

// Notion opens elsewhere with ↗ (a new tab, the library, a link; seen 2026-10-07).
export const OpenInNew = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUpRight} glyph="open-in-new" />
);

export const OpenAsPage = ({
  direction = "out",
  style,
  ...p
}: LeafenIconProps & { direction?: "in" | "out" }) => {
  const transform = style?.transform
    ? `${style.transform} scaleX(-1)`
    : "scaleX(-1)";
  return (
    <PhosphorIconSvg
      {...p}
      icon={direction === "in" ? PhosphorArrowsInSimple : PhosphorArrowsOutSimple}
      glyph={direction === "in" ? "arrows-in-simple" : "arrows-out-simple"}
      data-leafen-icon-mirror="horizontal"
      style={{
        ...style,
        transform,
        transformOrigin: style?.transformOrigin ?? "center",
      }}
    />
  );
};

/** Two arrows meeting inward, Notion's way out of its picture view (seen 2026-11-05). */
export const CollapseIcon = (p: LeafenIconProps) => <OpenAsPage {...p} direction="in" />;

/** A filled triangle with "!", Notion's mark on a link its embed can't draw (seen 2026-11-05). */
export const WarningTriangleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorWarning} weight="fill" />
);

export { DragHandleIcon } from "./dragHandleIcon";
export { Plus } from "./plusIcon";
export { Search } from "./searchIcon";

/** Bringing a page back from the trash, as Notion's trash row shows it. */
// Notion restores with a U-turn (its trash's "페이지 복원", seen 2026-10-07);
// a reset goes back around.
export const RestoreIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUUpLeft} glyph="restore" />
);

/** The Enter key, as a list shows what Enter picks (Notion's "↵"). */
export const ReturnKeyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowElbowDownLeft} />
);

/** One arrow round, as Notion's "다시 열기" on a resolved discussion (seen 2026-11-06). */
export const ReopenIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowClockwise} />
);

export const ResetIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowCounterClockwise} />
);

/** Content coming back from where it moved, as Notion's restore lists it. */
export const ArrowBendUpLeftIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowBendUpLeft} />
);

/** Passing a row to someone (a row page's "보내기"). */
export const SendIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPaperPlaneTilt} />
);

export const Star = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorStar} />
);

export const StarFilled = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorStar} weight="fill" />
);

export const HeartIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorHeart} />
);

export const ClockIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorClock} />
);

export const AlarmIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorAlarm} />
);

export const InfinityIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorInfinity} />
);

/** A wide card with a plus, as Notion's "속성 그룹에 추가" (rectangleWidePlus, seen 2026-11-01). */
export const RectangleWidePlusIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="rectangle-wide-plus">
    <rect x="2.8" y="5.4" width="14.4" height="9.2" rx="1.8" />
    <path d="M10 7.8v4.4" />
    <path d="M7.8 10h4.4" />
  </IconSvg>
);

export const RepeatIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorRepeat} />
);

/** Stacked squares with a plus, as a filter's "필터 그룹 추가". */
export const StackPlusIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorStackPlus} />
);

/** The account's security, as the passkey suggestion's mark (Notion's shield). */
export const ShieldCheckIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorShieldCheck} />
);

/** A password field's mark. */
export const PasswordIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPassword} />
);

export const AudioIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSpeakerHigh} />
);

/** A sound's wave, as Notion's empty audio block marks it (seen 2026-11-01). */
export const WaveformIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorWaveform} />
);

/** Two arrows round, as Notion's "바꾸기" under a new emoji's picture (seen 2026-11-01). */
export const ReplaceIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowsClockwise} />
);

/** A page with an arrow up, as Notion's empty file block marks it (seen 2026-11-01). */
export const FileUploadIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorFileArrowUp} />
);

/** A video that will not play: a play ring, as Notion's plate marks it. */
export const VideoUnplayableIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPlayCircle} />
);

export const VideoIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="video">
    <rect x="3.8" y="5" width="12.4" height="10" rx="1.7" />
    <path d="m8.4 8 3.7 2-3.7 2Z" />
  </IconSvg>
);

export const BookmarkIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="bookmark">
    <path d="M5.3 4.2c0-.8.6-1.4 1.4-1.4h6.6c.8 0 1.4.6 1.4 1.4v12.6L10 13.7l-4.7 3.1Z" />
  </IconSvg>
);

export const AlignLeftIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="align-left">
    <path d="M4 5h12" />
    <path d="M4 10h8" />
    <path d="M4 15h12" />
  </IconSvg>
);

export const SyncIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="sync">
    <path d="M15 6.4A5.6 5.6 0 0 0 5.5 5.8L4 7.3" />
    <path d="M4 4v3.3h3.3" />
    <path d="M5 13.6a5.6 5.6 0 0 0 9.5.6L16 12.7" />
    <path d="M16 16v-3.3h-3.3" />
  </IconSvg>
);

/** A filled circle with an x, as a filter's "clear" shows: Notion's xMarkCircleFill, its circle 15.25 of 20 where Phosphor's is 16.25 (seen 2026-11-17). */
export const XCircleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorXCircle} weight="fill" crop={15.25 / 16.25} />
);

/** A circle drawn round an x, as a table cell menu's "콘텐츠 삭제" shows. */
export const XCircleOutlineIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorXCircle} />
);

/** A circle with an "i", as a described property's head shows. */
export const InfoIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorInfo} />
);

/** A filled circle with a check, as the band after a restore shows. */
export const CheckCircleFillIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCheckCircle} weight="fill" />
);

/** A filled circle with an "i", as the button that describes a property shows. */
export const InfoFillIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorInfo} weight="fill" />
);

// The sidebar's marks, as Notion draws them (seen 2026-10-07): a square with
// a pencil for a new page, ↗ to open in the library, a filled box on an
// archived page, and four dots for its "더 보기".
export const ComposeIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorNotePencil} />
);

export const ArrowUpLeftIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUpLeft} />
);
export const ArrowUpRightIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUpRight} />
);

export const ArchivedMarkIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArchive} weight="fill" />
);

export const MoreAppsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCirclesFour} />
);

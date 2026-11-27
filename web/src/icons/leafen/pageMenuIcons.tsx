import { useId } from "react";
import {
  ArrowCircleUp as PhosphorArrowCircleUp,
  ArrowElbowDownRight as PhosphorArrowElbowDownRight,
  ArrowsMerge as PhosphorArrowsMerge,
  ArrowsSplit as PhosphorArrowsSplit,
  ArrowLineDown as PhosphorArrowLineDown,
  ArrowLineUp as PhosphorArrowLineUp,
  ArrowUUpLeft as PhosphorArrowUUpLeft,
  ArrowsCounterClockwise as PhosphorArrowsCounterClockwise,
  ArrowsHorizontal as PhosphorArrowsHorizontal,
  ArrowsInSimple as PhosphorArrowsInSimple,
  ArrowsOutSimple as PhosphorArrowsOutSimple,
  BracketsCurly as PhosphorBracketsCurly,
  CaretUpDown as PhosphorCaretUpDown,
  LightningSlash as PhosphorLightningSlash,
  Export as PhosphorExport,
  HandPalm as PhosphorHandPalm,
  HandWaving as PhosphorHandWaving,
  Image as PhosphorImage,
  Key as PhosphorKey,
  PencilLine as PhosphorPencilLine,
  PaintRoller as PhosphorPaintRoller,
  ShieldCheck as PhosphorShieldCheck,
  SquaresFour as PhosphorSquaresFour,
  UserCirclePlus as PhosphorUserCirclePlus,
  UserGear as PhosphorUserGear,
  TextAa as PhosphorTextAa,
  type Icon as PhosphorIcon,
} from "@phosphor-icons/react";
import { IconSvg, iconPixels, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

/** A phone path sheet's row under the first: ↳ before the page's own mark, as Notion's "이동 경로" (seen 2026-11-17). */
export const PathChildIcon = (p: LeafenIconProps) => <PhosphorIconSvg {...p} icon={PhosphorArrowElbowDownRight} />;

/** Phone page share: an arrow rises from the sheet. */
/** "게시" for a page in the trash: a globe struck through, as Notion's (seen 2026-11-13). */
export const GlobeOffIcon = (p: LeafenIconProps) => <IconSvg {...p} glyph="globe-off">
  <circle cx="10" cy="10" r="6.5" />
  <path d="M3.5 10h13M10 3.5c-1.8 1.8-2.6 4-2.6 6.5s.8 4.7 2.6 6.5c1.8-1.8 2.6-4 2.6-6.5s-.8-4.7-2.6-6.5M3.6 3.6l12.8 12.8" />
</IconSvg>;

export const PhoneShareIcon = (p: LeafenIconProps) => <IconSvg {...p} glyph="phone-share">
  <path d="M7 7H5a1 1 0 0 0-1 1v8a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V8a1 1 0 0 0-1-1h-2M10 13V3m-3 3 3-3 3 3" />
</IconSvg>;

// A page's "…" menu, as Notion draws it (seen 2026-10-07): export rises from
// a line and import falls to one, connections a grid of four, small text an A with a falling arrow, full width ↔, and a
// suggestion a speech bubble with a pencil.
export const ExportIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowLineUp} />
);

/** "실행 취소": an arrow turning back up and left, as Notion's. */
export const UndoIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowUUpLeft} />
);

export const ImportIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowLineDown} />
);

/** A down arrow over a line: Notion's cover "download" (arrowLineDown, seen 2026-11-07). */
export const ArrowLineDownIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowLineDown} />
);

export const ConnectionsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSquaresFour} />
);

export const FullWidthIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowsHorizontal} />
);

/** "테마와 레이아웃": an "Aa", as Notion's (seen 2026-10-17). */
export const ThemeLayoutIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTextAa} />
);

export const SmallTextIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="small-text">
    <path d="m3.6 15.4 3.6-10.8 3.6 10.8" />
    <path d="M4.9 11.6h4.6" />
    <path d="M14.6 5v10" />
    <path d="m12.4 12.8 2.2 2.4 2.2-2.4" />
  </IconSvg>
);

/**
 * "편집 내용 제안": a speech box, its tail dropping from the lower start, with
 * a solid pencil crossing its upper end corner, as Notion's block menu and
 * selection bar draw it (read 2026-11-16 at 1440 in dark on its 20 box;
 * overlap 0.92), not from Notion's path.
 */
export const SuggestEditIcon = (p: LeafenIconProps) => {
  // Its lines stay 1.25px however small it is drawn (a face's 10px badge), as every hand-drawn mark's do.
  const unit = 20 / (p.pixels ?? iconPixels(p.size));
  return (
    <IconSvg {...p} viewBox="0 0 20 20" glyph="suggest-edit" strokeWidth={1.25 * unit}>
      <path d="M13 3.3H4.6A1.6 1.6 0 0 0 3 4.9v7.4a1.6 1.6 0 0 0 1.6 1.6h1.7v3l3.5-3h5.6a1.6 1.6 0 0 0 1.6-1.6V6" />
      <path d="M18.37 2.02 10.17 10.23 8.9 10.65 9.32 9.38 17.53 1.18Z" fill="currentColor" strokeWidth={0.4 * unit} />
    </IconSvg>
  );
};

/** A block's "색", as Notion's block menu draws it. */
export const PaintRollerIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPaintRoller} />
);

// The settings window's sections, as Notion's (seen 2026-10-07): {} for
// developers, a building for teamspaces, a key for security, a shield with a
// check for identity.
export const DeveloperIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBracketsCurly} />
);

// Notion's teamspace mark (its "teamspace" glyph, the same in the trash's
// filter pill and the search window's filter menu, seen 2026-11-16): an office
// block with two rows of two windows over a door, a lower wing on its right
// with two windows, 1.25 lines on a 20 grid spanning 2.5–17.5 across; drawn
// 18 wide and 20 tall where a pill trims it to its ink, as Notion's.
const TEAMSPACE_WINDOWS = [[5.48, 6.96], [8.5, 6.96], [5.48, 9.98], [8.5, 9.98], [14.53, 9.98], [14.53, 12.99]] as const;

export const TeamspaceIcon = ({ trim, ...p }: LeafenIconProps) => {
  const pixels = p.pixels ?? iconPixels(p.size ?? "small");
  return <IconSvg {...p} glyph="teamspace" viewBox={trim ? "1 0 18 20" : "0 0 20 20"} {...(trim ? { width: (pixels * 18) / 20 } : {})}>
    <path strokeWidth="1.25" d="M2.47 16.08V5.28a1.35 1.35 0 0 1 1.35-1.35h6.34a1.35 1.35 0 0 1 1.35 1.35v10.8M11.51 6.95h4.68a1.35 1.35 0 0 1 1.35 1.35v7.78H2.47" />
    <path strokeWidth="1.1" d="M5.48 15.5v-2.51h3.02v2.51" />
    {TEAMSPACE_WINDOWS.map(([cx, cy]) => <circle key={`${cx},${cy}`} cx={cx} cy={cy} r="0.68" fill="currentColor" stroke="none" />)}
  </IconSvg>;
};

/** A site search engines list: a browser window, a magnifying glass at its corner, as Notion's "웹에서 검색 가능" (seen 2026-11-12). */
export const SiteSearchableIcon = (p: LeafenIconProps) => <IconSvg {...p} glyph="site-searchable">
  <path d="M9.6 15.5H4.5a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2h11a2 2 0 0 1 2 2v3.6M2.5 7h15M15.9 15.9l2 2" />
  <circle cx="14" cy="14" r="2.7" />
</IconSvg>;

// Notion's person (its role menus' and developer link's marks, measured
// 2026-11-12): an oval head over a bust with a flat, rounded foot, 1.25 lines.
const PERSON_HEAD = { cx: 10, cy: 6.3, rx: 2.95, ry: 3.2 } as const;
const PERSON_BUST = "M5.4 17.1h9.2c.75 0 1.3-.7 1.1-1.42C15.1 13.85 12.8 12.5 10 12.5s-5.1 1.35-5.7 3.18c-.2.72.35 1.42 1.1 1.42z";

/** A person, as Notion's role menus mark a member. */
export const PersonIcon = (p: LeafenIconProps) => <IconSvg {...p} viewBox="0 0 20 20" glyph="person">
  <g strokeWidth="1.25"><ellipse {...PERSON_HEAD} /><path d={PERSON_BUST} /></g>
</IconSvg>;

/**
 * Notion's person as its settings filters set it beside their words (the
 * teamspaces' "소유자", seen 2026-11-16 at 1440, where its ink sits): its
 * head a taller oval than the role menus' (as the "@" menu's 초대) over the
 * same bust, in a box as wide as the glyph, 12.64 of its 20 across
 * (3.68–16.32): 12.63×20 at 20px.
 */
export const PersonFilterIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="3.68 0 12.64 20" width={((p.pixels ?? iconPixels(p.size)) * 12.64) / 20} overflow="visible" glyph="person-filter">
    <g strokeWidth="1.25"><ellipse cx="10" cy="6.44" rx="2.95" ry="3.44" /><path d={PERSON_BUST} /></g>
  </IconSvg>
);

/** A person struck through, as Notion's "워크스페이스에서 제거": a gap parts the person on the slash's upper side. */
export const PersonSlashIcon = (p: LeafenIconProps) => {
  const cut = `person-slash-${useId().replace(/:/g, "")}`;
  return <IconSvg {...p} viewBox="0 0 20 20" glyph="person-slash">
    <defs>
      <mask id={cut} maskUnits="userSpaceOnUse" x="0" y="0" width="20" height="20">
        <rect x="0" y="0" width="20" height="20" fill="#fff" stroke="none" />
        <path d="M3.4 0.4L19.4 19.1" stroke="#000" strokeWidth="1.3" strokeLinecap="butt" />
      </mask>
    </defs>
    <g strokeWidth="1.25">
      <g mask={`url(#${cut})`}><ellipse {...PERSON_HEAD} /><path d={PERSON_BUST} /></g>
      <path d="M4 3l12.7 14.85" />
    </g>
  </IconSvg>;
};

/** A person with a key: a workspace's owner in Notion's role menus, and a personal access token as its developer link (seen 2026-10-30, 2026-11-12). */
export const PersonKeyIcon = (p: LeafenIconProps) => {
  const cut = `person-key-${useId().replace(/:/g, "")}`;
  return <IconSvg {...p} viewBox="0 0 20 20" glyph="person-key">
    <defs>
      <mask id={cut} maskUnits="userSpaceOnUse" x="0" y="0" width="20" height="20">
        <rect x="0" y="0" width="20" height="20" fill="#fff" stroke="none" />
        <circle cx="16.2" cy="12.6" r="4" fill="#000" stroke="none" />
        <rect x="13.7" y="12.6" width="6.3" height="7.4" fill="#000" stroke="none" />
      </mask>
    </defs>
    <g strokeWidth="1.25"><ellipse {...PERSON_HEAD} /><path d={PERSON_BUST} mask={`url(#${cut})`} /></g>
    <g fill="currentColor" stroke="none">
      <path fillRule="evenodd" d="M16.2 9.9a2.7 2.7 0 1 1 0 5.4a2.7 2.7 0 1 1 0-5.4zM16.2 11.35a.75.75 0 1 0 0 1.5a.75.75 0 1 0 0-1.5z" />
      <path d="M15 14.6h2.4v2l-.9.85.85.95-1.15 1.3L15 18.55z" />
    </g>
  </IconSvg>;
};

/**
 * Inviting someone, as Notion's "@" menu's "초대" row (seen 2026-11-12): a
 * person, its head a taller oval than the role menus', whose bust gives way at
 * its lower right to a round badge with a plus cut out of it.
 */
export const PersonInviteIcon = (p: LeafenIconProps) => {
  const cut = `person-invite-${useId().replace(/:/g, "")}`;
  return <IconSvg {...p} viewBox="0 0 20 20" glyph="person-invite">
    <defs>
      <mask id={cut} maskUnits="userSpaceOnUse" x="0" y="0" width="20" height="20">
        <rect x="0" y="0" width="20" height="20" fill="#fff" stroke="none" />
        <path d="M16 13.97v4.06M13.97 16h4.06" stroke="#000" strokeWidth="1.25" />
      </mask>
    </defs>
    <g strokeWidth="1.25">
      <ellipse cx="10" cy="6.44" rx="2.95" ry="3.44" />
      <path d="M11.1 17H5.85c-.95 0-1.55-.8-1.18-1.53C5.55 13.75 7.6 12.25 10 12.25c.9 0 1.75.15 2.45.43" />
    </g>
    <circle cx="16" cy="16" r="3.5" fill="currentColor" stroke="none" mask={`url(#${cut})`} />
  </IconSvg>;
};

/** An MCP token: three nodes joined by dotted lines, as Notion's developer link (seen 2026-10-30). */
export const McpTokenIcon = (p: LeafenIconProps) => <IconSvg {...p} glyph="mcp-token">
  <circle cx="6" cy="6" r="2.2" />
  <circle cx="14" cy="6" r="2.2" />
  <circle cx="10" cy="14" r="2.2" />
  <path d="M8.7 6h2.6M7.2 8.4l1.5 3M12.8 8.4l-1.5 3" strokeDasharray="0.01 1.6" />
</IconSvg>;

/**
 * An empty page as Notion's settings filters set it beside their words (the
 * verified pages' "다음에 포함", seen 2026-11-16 at 1440): a page 11.75 wide
 * and 15.25 tall with its top right corner folded, in a box as wide as the
 * page, 4.125–15.875 of its 20 across: 11.75×20 at 20px.
 */
export const EmptyPageFilterIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} viewBox="4.125 0 11.75 20" width={((p.pixels ?? iconPixels(p.size)) * 11.75) / 20} overflow="visible" glyph="empty-page-filter">
    <path strokeWidth="1.25" d="M6.35 3h4.9l4 4v8.4a1.6 1.6 0 0 1-1.6 1.6H6.35a1.6 1.6 0 0 1-1.6-1.6V4.6A1.6 1.6 0 0 1 6.35 3zM11.25 3v2.9a1.1 1.1 0 0 0 1.1 1.1h2.9" />
  </IconSvg>
);

/**
 * A key standing upright, as the teamspaces' "공개 수준" filter in Notion's
 * settings (seen 2026-11-16 in Chrome 1440: a 20×20 mark): a ring bow with
 * a filled hole over a shank that ends in a point, notched once on its right.
 */
export const UprightKeyIcon = (p: LeafenIconProps) => <IconSvg {...p} viewBox="0 0 20 20" glyph="upright-key">
  <path strokeWidth="1.25" d="M8.05 10.53A4.35 4.35 0 1 1 11.8 10.53V13.35L10.75 14.6L11.8 15.85L9.92 17.5L8.05 15.85Z" />
  <circle cx="9.92" cy="5.65" r="0.97" fill="currentColor" stroke="none" />
</IconSvg>;

export const SecurityKeyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorKey} />
);

export const IdentityIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorShieldCheck} />
);

/** "계정 추가하기", as Notion's workspace menu: a person in a circle with a plus. */
export const AddAccountIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUserCirclePlus} />
);

/** A photo, as Notion's "커버 추가" (drawn filled there). */
export const PhotoIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorImage} />
);

/** Leaving a teamspace, as Notion's: a waving hand. */
export const LeaveIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorHandWaving} />
);

/** Editing a name in place, as Notion's teamspace settings: a pencil over a line. */
export const RenameInPlaceIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorPencilLine} />
);

/** Image crop tool, with the two crossing corners of the native catalog. */
export const CropIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="crop"><path d="M5.5 2.75V14.5h11.75M2.75 5.5H14.5v11.75" /></IconSvg>
);

/** A published site's "소셜 미디어에 사이트 공유": a box with an arrow up out of it. */
export const ShareSiteIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorExport} />
);

/** A share window's teamspace row opens to its owners and members: ⇕, as Notion's. */
export const ChevronUpDown = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorCaretUpDown} />
);

// The workspace role menu's owner and "워크스페이스에서 제거" rows.
export const UserGearIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUserGear} />
);

/** A guest's "멤버로 업그레이드": an arrow up in a circle, as Notion's (seen 2026-11-09). */
export const UpgradeCircleIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowCircleUp} />
);

/** A guest's "워크스페이스에서 제거": an open hand, as Notion's (seen 2026-11-09). */
export const HandPalmIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorHandPalm} />
);

/** "프레젠테이션 모드" and "여기에서 발표": a screen with a play mark, as Notion's (seen 2026-10-12). */
export const PresentIcon = (p: LeafenIconProps) => <IconSvg {...p} glyph="present">
  <rect x="3.5" y="4.5" width="13" height="11" rx="2" />
  <path d="M8.5 7.8v4.4l3.6-2.2z" />
</IconSvg>;

/** The presentation's "전체 화면으로 보기": arrows out to the top left and bottom right corners, as Notion's (seen 2026-11-09). */
export const PresentFullscreenIcon = (p: LeafenIconProps) => (
  <PresentArrows {...p} icon={PhosphorArrowsOutSimple} glyph="arrows-out-simple" />
);

/** Its "전체 화면 종료" while the presentation fills the screen: the same arrows turned in, as Notion's (seen 2026-11-09). */
export const PresentFullscreenExitIcon = (p: LeafenIconProps) => (
  <PresentArrows {...p} icon={PhosphorArrowsInSimple} glyph="arrows-in-simple" />
);

// Phosphor's arrows run to the top right and bottom left; Notion's to the top left and bottom right.
function PresentArrows({ style, ...p }: LeafenIconProps & { icon: PhosphorIcon; glyph: string }) {
  return <PhosphorIconSvg {...p} data-leafen-icon-mirror="horizontal"
    style={{ ...style, transform: style?.transform ? `${style.transform} scaleX(-1)` : "scaleX(-1)", transformOrigin: style?.transformOrigin ?? "center" }} />;
}

/** Over the question to unlink sharing: an arrow parting in two upward, as Notion's
 * (seen 2026-11-06, its direction 2026-11-07): Phosphor's parts downward, so it is turned over. */
export const UnlinkSharingIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} style={{ ...p.style, transform: "scaleY(-1)" }} icon={PhosphorArrowsSplit} glyph="arrows-split" />
);

/** "연결 해제" in a page's "연결 ›": a bolt struck through, as Notion's (seen 2026-11-21, its lightningSlash). */
export const DisconnectConnectionIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorLightningSlash} glyph="lightning-slash" />
);

/** Over "공유 설정을 다시 연결할까요?": arrows coming together upward, as Notion's
 * (seen 2026-11-06, its direction 2026-11-07): Phosphor's meet downward, so it is turned over. */
export const RelinkSharingIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} style={{ ...p.style, transform: "scaleY(-1)" }} icon={PhosphorArrowsMerge} glyph="arrows-merge" />
);

/**
 * "CSV와 병합": an arrow up whose shaft parts in two below, each part bending
 * out to a lower corner, as Notion's (seen 2026-11-07, its 20-unit art read on
 * this 16-unit box).
 */
export const MergeCsvIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="merge-csv">
    <path d="M6.6 7.4 10 4l3.4 3.4" />
    <path d="M10 4.2v6.7c0 2.3-1.7 3.7-4 4.5" />
    <path d="M10 10.9c0 2.3 1.7 3.7 4 4.5" />
  </IconSvg>
);

/** Two arrows round, as Notion's version history "복원 시작" (seen 2026-11-06). */
export const VersionRestoreIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowsCounterClockwise} glyph="version-restore" />
);

/**
 * The analytics tab with no views yet: a magnifying glass with a four-point
 * sparkle in its lens, as Notion's (seen 2026-11-11).
 */
export const SparkleMagnifyingGlassIcon = (p: LeafenIconProps) => (
  <IconSvg {...p} glyph="sparkle-magnifying-glass">
    <circle cx="8.6" cy="8.6" r="5.4" />
    <path d="M12.5 12.5 16.8 16.8" />
    <path d="M8.6 5.9c.3 1.7.9 2.4 2.7 2.7-1.8.3-2.4 1-2.7 2.7-.3-1.7-.9-2.4-2.7-2.7 1.8-.3 2.4-1 2.7-2.7Z"
      fill="currentColor" stroke="none" />
  </IconSvg>
);

import {
  ArrowCircleUp as PhosphorArrowCircleUp,
  Buildings as PhosphorBuildings,
  UserSwitch as PhosphorUserSwitch,
  TreeStructure as PhosphorTreeStructure,
  ClipboardText as PhosphorClipboardText,
  HardDrives as PhosphorHardDrives,
  Swap as PhosphorSwap,
  ClockCounterClockwise as PhosphorClockCounterClockwise,
  SquaresFour as PhosphorSquaresFour,
  MapTrifold as PhosphorMapTrifold,
  SignIn as PhosphorSignIn,
  UsersThree as PhosphorUsersThree,
} from "@phosphor-icons/react";
import { PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// The server settings' section marks (Notion has no server settings; these
// follow its settings' kind of mark).
export const ServerUpdatesIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorArrowCircleUp} />
);

export const ServerWorkspacesIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorBuildings} />
);

// Settings › 서버 계정 (the accounts this device holds on other servers) and
// › 조직 (an organization's policies and domains), in the same kind of mark.
export const ServerAccountsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUserSwitch} />
);

export const OrganizationIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorTreeStructure} />
);

export const ServerAuditIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorClipboardText} />
);

export const ServerDataIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorHardDrives} />
);

export const ServerStandbyIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSwap} />
);

export const ServerRestoreIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorClockCounterClockwise} />
);

// Notion brings an archived teamspace back with a clock turning back (its
// settings table's "팀스페이스 복원", seen 2026-11-07), 14 wide in a 16 box
// beside a button's words (clockArrowBack, seen 2026-11-13).
export const TeamspaceRestoreIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorClockCounterClockwise} glyphWidth={14} />
);

export const ServerAppsIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSquaresFour} />
);

export const ServerMapIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorMapTrifold} />
);

export const ServerProvidersIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSignIn} />
);

// Signing in from a published site's "⋯", as Notion's (seen 2026-11-06): an arrow into a door.
export const SignInIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSignIn} />
);

export const ServerUsersIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorUsersThree} />
);

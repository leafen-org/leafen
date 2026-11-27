import type { TFunction } from "i18next";
import { administratorBounds, type AdministratorManagedAccount } from "../../administratorWire.generated";

const ROLE_KEYS = { owner: "users.placeOwner", member: "users.placeMember", restricted: "users.placeRestricted" } as const;

/**
 * Where an account is, as its row in 설정 › 서버 › 사용자 says it
 * (administrator_account_places.go): the workspaces it owns or is a member of
 * with its role, those it is a guest in with how many pages, then how many
 * more — or that it is in no workspace, which a count of the workspaces it
 * owns alone never said (2026-11-23: read as "in none" for a member).
 */
export function accountPlaces(account: AdministratorManagedAccount, tc: TFunction<"serverConsole">): string[] {
  const all = account.joinedWorkspaceCount + account.guestWorkspaceCount;
  if (!all) return [tc("users.placeNone")];
  const listed = [
    ...account.joinedWorkspaces.map((place) => tc(ROLE_KEYS[place.role], { workspace: place.name || place.id })),
    ...account.guestWorkspaces.map((place) => tc("users.placeGuest", { workspace: place.name || place.id, count: place.pageCount })),
  ].slice(0, administratorBounds.accountWorkspacesShown);
  const more = all - listed.length;
  return more > 0 ? [...listed, tc("users.placeMore", { count: more })] : listed;
}

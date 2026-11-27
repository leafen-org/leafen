export type WorkspaceMemberSortKey = "user" | "role";
export type WorkspaceMemberSort = { key: WorkspaceMemberSortKey; descending: boolean };

type SortableMember = { userId: string; role: string };

/**
 * A people table's order, as Notion's member table (seen 2026-10-10): by
 * user name, A to Z at first, or by role with the owner first (a table with
 * more roles ranks them by its own `rank`); pressing the sorted column again
 * turns it around. Equal names or roles keep a fixed order.
 */
export function sortWorkspaceMembers<Member extends SortableMember>(
  members: readonly Member[], sort: WorkspaceMemberSort, label: (member: Member) => string, locale?: string,
  rank: (member: Member) => number = (member) => (member.role === "owner" ? 0 : 1),
): Member[] {
  const collator = new Intl.Collator(locale, { sensitivity: "base", numeric: true });
  const byName = (left: Member, right: Member) => collator.compare(label(left), label(right))
    || (left.userId < right.userId ? -1 : left.userId > right.userId ? 1 : 0);
  const direction = sort.descending ? -1 : 1;
  return [...members].sort((left, right) => sort.key === "role"
    ? direction * (rank(left) - rank(right)) || byName(left, right)
    : direction * byName(left, right));
}

/** The next order after pressing a column's head. */
export function nextWorkspaceMemberSort(current: WorkspaceMemberSort, key: WorkspaceMemberSortKey): WorkspaceMemberSort {
  return current.key === key ? { key, descending: !current.descending } : { key, descending: false };
}

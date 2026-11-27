import type { WorkspaceActivityRecord } from "@/workspaceActivityWire.generated";

/**
 * The sentence a permission line reads with (inbox.sentences): access the
 * pages above gave restored — "본인이 〈page〉의 상속된 사용 권한을 복원함" —
 * or taken away — "본인이 〈page〉에 대한 사용 권한을 제한함", a share going
 * to no access (page_share_unlink_notices.go) — or else "본인 〈page〉의
 * 권한을 업데이트했습니다.", as Notion's workspace and page updates read them.
 */
export function permissionLineSentence(record: WorkspaceActivityRecord): "permission" | "permissionRestricted" | "permissionRestored" {
  if (record.kind === "restored") return "permissionRestored";
  const changes = record.permissions?.length ? record.permissions : [record];
  return changes.some((change) => change.toRole === "none") ? "permissionRestricted" : "permission";
}

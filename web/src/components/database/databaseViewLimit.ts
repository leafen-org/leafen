import { databaseViewLabels } from "./databaseViewLabels";

/** A database holds at most 100 views, as Notion's (the server's database_view_limit.go). */
export const DATABASE_VIEW_LIMIT = 100;

/**
 * Why a database at its limit makes no other view, or undefined below it:
 * what Notion's dimmed rows say as the pointer rests on them — the tabs' "새
 * 보기", "보기 복제" and "새 데이터 소스" (seen 2026-11-12), and "데이터 소스
 * 관리"'s "데이터 소스 추가" and "기존 데이터 소스 연결하기" (seen 2026-11-18
 * in Chrome: both at 0.4, a press on them doing nothing).
 */
export function viewLimitRefusal(viewCount: number): string | undefined {
  return viewCount >= DATABASE_VIEW_LIMIT ? databaseViewLabels().viewLimitReached : undefined;
}

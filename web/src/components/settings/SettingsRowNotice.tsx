import type { HTMLAttributes, ReactNode } from "react";
import { ReadAgain } from "@/components/ReadFailed";
import { styles as settingsStyles } from "./workspaceSettingsOverviewStyles";
import notice from "./SettingsRowNotice.module.css";

// What an action on one row of a settings list said, under that row — never
// a line under the whole list (lib/useNoticeFor useRowNotice holds it) — and
// what is said about a whole list or page, over the rows that follow it.

/** Right after a list row (SettingsItemRows .row), inside the row's line. */
export function RowNotice({ text, ...data }: { text: string | null } & { [data: `data-${string}`]: string | boolean }) {
  return text ? (
    <div className={notice.afterRow}>
      <div className={settingsStyles.errorText} role="alert" {...data}>{text}</div>
    </div>
  ) : null;
}

/** In a table: a row of its own under the row it is about, across every column. */
export function TableRowNotice({ text, columns }: { text: string | null; columns: number }) {
  return text ? (
    <tr className={notice.tableRow} data-row-notice>
      <td colSpan={columns}><div className={settingsStyles.errorText} role="alert">{text}</div></td>
    </tr>
  ) : null;
}

/**
 * About a whole list or page, over the rows that follow it — a list's first
 * row, the page's first group, the next such notice — and 12px above them, as
 * the settings notice (.notice) stands over a list. The notice form keeps no
 * room of its own (2026-11-20: 에이전트's capacity and reconnect notices touched
 * each other and the first connection). A list whose read failed says so here
 * with a way to read it again (onRetry, ReadAgain), in place of its loading
 * line, which goes (2026-11-20: 요청's "요청을 불러오는 중…" stayed under its
 * failure).
 */
export function ListNotice({ tone = "error", children, onRetry, ...rest }: {
  tone?: "error" | "notice"; children: ReactNode; onRetry?: () => void;
} & Omit<HTMLAttributes<HTMLDivElement>, "className" | "children"> & { [data: `data-${string}`]: string | boolean | undefined }) {
  return (
    <div className={`${tone === "error" ? settingsStyles.errorText : settingsStyles.noticeText} ${notice.overRows}${onRetry ? ` ${notice.readAgain}` : ""}`}
      data-read-failed={onRetry ? true : undefined} {...rest}>
      {onRetry ? <><span>{children}</span><ReadAgain onRetry={onRetry} /></> : children}
    </div>
  );
}

/**
 * A list's failed read where its empty line (.emptyState) would stand: 12px
 * under what is above it, as that line stands, the failure as ListNotice
 * says it with a way to read again.
 */
export function EmptyLineReadFailed({ children, onRetry, ...rest }: {
  children: ReactNode; onRetry: () => void;
} & { [data: `data-${string}`]: string | boolean | undefined }) {
  return <div className={notice.inEmptyLine}><ListNotice role="alert" onRetry={onRetry} {...rest}>{children}</ListNotice></div>;
}

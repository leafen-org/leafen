import type { Page, TextSpan } from "./types";

export interface ButtonExecutionConfirmation {
  actionId: string;
  title: string;
  message: string;
  /** The words with their mentions, as the run gave them. */
  messageSpans?: TextSpan[];
  confirmLabel: string;
  cancelLabel: string;
}

export interface ButtonConfirmationChallenge {
  executionId: string;
  replayed: boolean;
  insertedBlockIds?: string[];
  createdPageIds?: string[];
  editedPageIds?: string[];
  tasks?: boolean;
  clientOutcomes?: ButtonClientOutcome[];
  canUndo?: boolean;
  confirmationRequired: true;
  confirmationToken: string;
  confirmations: ButtonExecutionConfirmation[];
}

export interface ButtonClientOutcome {
  actionId: string;
  type: "focus_block" | "open_page" | "open_form" | "open_url";
  blockId?: string;
  pageId?: string;
  databaseId?: string;
  viewId?: string;
  /** An open form's database block, where its page holds it inline; an opened database a page holds, by its block. */
  containerId?: string;
  url?: string;
  /** Where an opened page shows: beside the page, over it (when none is named), or in its place. */
  openIn?: "side_peek" | "center_peek" | "full_page";
  /** An opened form's link for the one who pressed, when they may answer it. */
  formToken?: string;
}

export interface ExecuteDatabaseButtonCompletedResult {
  executionId: string;
  replayed: boolean;
  row?: Page;
  createdPages: Page[];
  updatedPages: Page[];
  clientOutcomes: ButtonClientOutcome[];
  /** The rows it made and changed, and whether they are all tasks, for its notice. */
  notice?: ButtonRunNotice;
}

export interface ButtonRunNotice {
  createdPageIds: string[];
  editedPageIds: string[];
  tasks: boolean;
  canUndo?: boolean;
}

export type ExecuteDatabaseButtonResult =
  | ButtonConfirmationChallenge
  | ExecuteDatabaseButtonCompletedResult;

export interface ExecutePageButtonCompletedResult {
  executionId: string;
  replayed: boolean;
  canUndo?: boolean;
  insertedBlockIds: string[];
  createdPageIds: string[];
  editedPageIds?: string[];
  tasks?: boolean;
  clientOutcomes: ButtonClientOutcome[];
}

export type ExecutePageButtonResult =
  | ButtonConfirmationChallenge
  | ExecutePageButtonCompletedResult;

export function buttonResultRequiresConfirmation(
  result: ExecuteDatabaseButtonResult | ExecutePageButtonResult,
): result is ButtonConfirmationChallenge {
  return "confirmationRequired" in result && result.confirmationRequired === true;
}

import type { Timestamped } from "./core";
import type { IconType, PageKind } from "./page";
import type { DatabaseSourceTitle } from "@/components/editor/databaseTitles";

export type BlockType =
  | "paragraph"
  | "heading_1"
  | "heading_2"
  | "heading_3"
  | "heading_4"
  | "toggle_heading_1"
  | "toggle_heading_2"
  | "toggle_heading_3"
  | "toggle_heading_4"
  | "bulleted_list_item"
  | "numbered_list_item"
  | "to_do"
  | "toggle"
  | "quote"
  | "callout"
  | "divider"
  | "code"
  | "equation"
  | "table"
  | "table_row"
  | "image"
  | "video"
  | "audio"
  | "bookmark"
  | "link_preview"
  | "embed"
  | "pdf"
  | "file"
  | "breadcrumb"
  | "table_of_contents"
  | "synced_block"
  | "button"
  | "template"
  | "tab"
  | "inline_database"
  | "column_list"
  | "column"
  | "child_page"
  | "link_to_page"
  | "child_database";

/** A run of text with formatting marks. */
export interface TextSpan {
  text: string;
  bold?: boolean;
  italic?: boolean;
  underline?: boolean;
  strikethrough?: boolean;
  code?: boolean;
  color?: string; // text color, e.g. "red"; an older span may hold its background here
  /** Its background, e.g. "blue_background", kept apart from its text color as in Notion. */
  background?: string;
  link?: string;
  commentId?: string;
  mention?: "page" | "database" | "custom_emoji" | "date" | "person" | "external" | "template" | "automation";
  pageId?: string;
  /** A page mention's block on that page, which it names and opens at, as Notion's link to a block. */
  blockId?: string;
  databaseId?: string;
  customEmojiId?: string;
  date?: string;
  /** A date mention whose start was never given, as Notion's "v1" Markdown can write one: its date is none, or a clock alone. */
  dateStartUnset?: boolean;
  dateEnd?: string;
  dateTimeZone?: string;
  /** Minutes before a date mention (9:00 that day for a date alone) its reminder fires. */
  dateReminder?: string;
  /** How a date mention shows its date (DATE_MENTION_FORMATS); relative when absent. */
  dateFormat?: string;
  /** How a date mention with a time shows it: "24" or "hidden"; 12 hours without. */
  dateTimeFormat?: string;
  userId?: string;
  iconUrl?: string;
  /**
   * What a mention of an outside page keeps of its preview, as Notion's link
   * mention: the site's name, drawn grey before its title, and the page's
   * description and picture, which its card shows.
   */
  provider?: string;
  description?: string;
  imageUrl?: string;
  equation?: string;
  /**
   * A piece with no characters, as Notion keeps one: an inline equation with
   * no expression, or a text piece with no words (a code block page Markdown
   * made with nothing in it). Its text is "", and the block's collaborative
   * text holds it as an embed (collaborationCrdtCodec).
   */
  emptyPiece?: "equation" | "text";
  /**
   * A database template's "when duplicated" mention: today, now or the person
   * using the template, which take their value when it makes a page.
   */
  template?: "today" | "now" | "me";
  /**
   * An automation message's mention of what its run gives — the page that
   * triggered it, one of its properties, who ran it, when, a variable — as
   * the formula that reads it (triggerPage.prop("Name")), with the property
   * type its chip's icon is; its text is the chip's name.
   */
  automation?: string;
  automationType?: string;
  /**
   * A suggested edit this text belongs to: text suggested in ("insert") or
   * away ("delete"), or text whose formatting is suggested ("format"), with
   * the comment thread that holds it and, for formatting, the formatting it
   * had before as JSON.
   */
  suggestion?: "insert" | "delete" | "format";
  suggestionId?: string;
  suggestionBefore?: string;
  /** A published page's mention of a page or database the visitor cannot open. */
  unavailable?: boolean;
  /** A published page's page mention: the icon of the page it names, as the visitor reads it. */
  mentionIcon?: string;
  mentionIconType?: string;
  /** A published page's page mention: whether the page it names holds anything, which its icon shows without one. */
  mentionHasContent?: boolean;
  /**
   * A published page's database mention: the page holding the database, "database" when it is the database's own;
   * its page mention: "database" for a full-page database's page, which names it while untitled, as mentionViewBlock.
   */
  mentionPageId?: string;
  mentionPageKind?: string;
  mentionViewBlock?: boolean;
  /**
   * A published page's page mention of an untitled full-page database of several sources: their titles, which name it
   * "A 및 B", one it shows from another database marked linked ("X 보기").
   */
  mentionSourceTitles?: DatabaseSourceTitle[];
  /** A published page's workspace emoji: the token its picture is served for to visitors. */
  customEmojiPublic?: string;
}

/**
 * Every field of a span but its text — its formatting, link, mention and
 * marks — for code that copies a span's fields or tells two spans apart
 * (joining like neighbours, a formula's rich text). The type below fails to
 * compile while a field of TextSpan is missing here, so a new field (a
 * background kept apart from the color, 2026-10-10) is never dropped.
 */
type TextSpanField = Exclude<keyof TextSpan, "text">;
function everyTextSpanField<const T extends readonly TextSpanField[]>(
  fields: T & ([Exclude<TextSpanField, T[number]>] extends [never] ? unknown : { missing: Exclude<TextSpanField, T[number]> }),
): T {
  return fields;
}
export const TEXT_SPAN_FIELDS = everyTextSpanField([
  "bold", "italic", "underline", "strikethrough", "code", "color", "background", "link", "commentId", "mention", "pageId",
  "blockId", "databaseId", "customEmojiId", "date", "dateStartUnset", "dateEnd", "dateTimeZone", "dateReminder", "dateFormat", "dateTimeFormat", "userId",
  "iconUrl", "provider", "description", "imageUrl", "equation", "emptyPiece", "template", "automation", "automationType", "suggestion", "suggestionId", "suggestionBefore", "unavailable",
  "mentionIcon", "mentionIconType", "mentionHasContent", "mentionPageId", "mentionPageKind", "mentionViewBlock", "mentionSourceTitles",
  "customEmojiPublic",
]);

/** type-specific content stored in blocks.content (json) */
export interface BlockContent {
  rich?: TextSpan[]; // text-bearing blocks
  checked?: boolean; // to_do
  collapsed?: boolean; // toggle / toggle heading
  language?: string; // code
  lineNumbers?: boolean; // code: show line-number gutter
  wrap?: boolean; // code: soft-wrap long lines
  expression?: string; // equation
  color?: string; // callout / colored blocks
  icon?: string; // callout icon
  url?: string; // image / video / audio / bookmark / link_preview / embed / pdf / file
  source?: "external"; // external file source (managed files use fileId)
  name?: string; // external file display name
  fileName?: string; // file
  caption?: TextSpan[]; // image / code caption
  showCaption?: boolean; // media/embed/file caption is explicitly enabled
  altText?: string; // image accessibility description
  imageLink?: string; // destination opened when an image is clicked
  align?: "left" | "center" | "right"; // media alignment
  childPageId?: string; // child_page / link_to_page / child_database / inline_database link
  sourcePageHref?: string; // imported page-link fallback when the source id is absent locally
  childPageTitle?: string; // imported linked page/database title snapshot
  childPageIcon?: string; // imported linked page/database icon snapshot
  childPageIconType?: IconType; // imported linked page/database icon type snapshot
  childPageKind?: PageKind; // imported linked target kind snapshot
  pageLinkPickerDismissed?: boolean; // an empty link_to_page an older build left shut (read only; its picker stays shut)
  databaseViewId?: string; // inline_database linked view
  databaseViewIds?: string[]; // inline_database block-scoped view tabs
  linkedDatabaseSource?: boolean; // inline_database links an existing database source
  sourceContainerId?: string; // canonical source container for a linked database
  linkedTitle?: string; // a linked database's own name, apart from its source's
  linkedTitleSpans?: TextSpan[]; // that name's formatting, spelling it
  pageTitleSpans?: TextSpan[]; // a full-page database's name's formatting, where its page's title names it
  linkedIcon?: string; // a linked database's own icon
  linkedIconType?: IconType; // a linked database's own icon type
  linkedMembers?: Array<{ dataSourceId: string; sourceContainerId: string }>; // inline_database sources linked from other databases
  autoFocusDatabaseTitle?: boolean; // transient inline_database title focus handoff
  templateSelfFilter?: {
    sourceDatabaseId?: string;
    relationPropertyId?: string;
  };
  width?: number; // column ratio / media width percent
  tableWidth?: number; // table
  hasColumnHeader?: boolean; // table
  hasRowHeader?: boolean; // table
  cells?: TextSpan[][]; // table_row
  syncedBlockId?: string; // synced_block copy source
  syncedPageId?: string; // synced_block copy source page
  text?: string; // a button's inserted paragraph, as the server adds it
  buttonLabel?: string; // button
  buttonTemplate?: ButtonTemplateBlock[]; // button
  buttonActionDocument?: PageButtonActionDocument; // button shared server action authority
  notionButtonPartial?: boolean; // imported button whose Notion API action/label details were hidden
  notionBlock?: unknown; // imported Notion block metadata used for normalized rendering
}

export interface ButtonTemplateBlock {
  id?: string;
  type: BlockType;
  content?: BlockContent;
  children?: ButtonTemplateBlock[];
}

export interface Block extends Timestamped {
  pageId: string;
  parentId?: string | null;
  type: BlockType;
  content?: BlockContent;
  plainText?: string;
  position: number;
  createdBy?: string;
  /** Server-authenticated actor for the latest persisted edit. */
  lastEditedBy?: string;
  /** Immutable request identity for exact block-create replay. */
  createMutationId?: string;
  /** Server receipt for update replay/idempotency; not user-authored content. */
  lastMutationId?: string;
  /** Server-owned retry anchor for a durable subtree move/delete. */
  structuralLifecycleJobId?: string | null;
}

export type AutomationValueExpression =
  /**
   * Words with mentions in them keep them beside their text (interactive_automation_message.go).
   * People ({ source } among their IDs) and a multi-select's options may be added to what the
   * property holds, taken from it or toggled in it rather than put in its place
   * (interactive_automation_people.go).
   */
  | { kind: "literal"; value: unknown; spans?: TextSpan[]; mode?: "add" | "remove" | "toggle" }
  | { kind: "execution_time" }
  /** The day the run began, without a time: Notion's "자동 실행된 날짜". */
  | { kind: "execution_date" }
  | { kind: "formula"; expression: string }
  | { kind: "variable"; name: string }
  | { kind: "trigger_property"; propertyId: string };

export type AutomationDynamicText = string | AutomationValueExpression;

export interface EditPropertyAutomationAction {
  id: string;
  type: "edit_property";
  target: "trigger_page";
  propertyId: string;
  value: AutomationValueExpression;
}

export interface DatabaseButtonActionDocument {
  version: 1;
  label: string;
  actions: AutomationAction[];
}

export interface InsertBlocksAutomationAction {
  id: string;
  type: "insert_blocks";
  target: "trigger_page";
  placement?: "button_above" | "button_below" | "page_top" | "page_bottom";
  sourceBlockId?: string;
  blocks?: ButtonTemplateBlock[];
}

export interface AddPageAutomationAction {
  id: string;
  type: "add_page";
  target: "database";
  databaseId: string;
  title: AutomationDynamicText;
  properties?: AutomationPropertyChange[];
  /** The template its page starts from; none is an empty page ("비어 있음"). */
  templateId?: string;
}

export interface AutomationPropertyChange {
  propertyId: string;
  value: AutomationValueExpression;
}

export interface EditPagesAutomationAction {
  id: string;
  type: "edit_pages";
  target: {
    /** "this_page": the row the button is on, in its database. */
    type: "database" | "this_page";
    databaseId: string;
    filter: Record<string, unknown>;
    limit?: number;
    timeZone?: string;
  };
  changes: AutomationPropertyChange[];
}

/** Archives the pages it names, Notion's "페이지 보관하기…": those an edit would name, and nothing set on them. */
export interface ArchivePagesAutomationAction {
  id: string;
  type: "archive_pages";
  target: EditPagesAutomationAction["target"];
}

/** Sets the icon of the page it ran for, Notion's "페이지 아이콘 편집…": an emoji, a built-in icon or the workspace's emoji
 *  (its id); none until chosen. */
export interface SetPageIconAutomationAction {
  id: string;
  type: "set_page_icon";
  target: "trigger_page";
  icon: string;
  iconType: "" | "emoji" | "icon" | "custom_emoji";
}

export interface DefineVariablesAutomationAction {
  id: string;
  type: "define_variables";
  variables: Array<{ name: string; value: AutomationValueExpression }>;
}

export interface SendNotificationAutomationAction {
  id: string;
  type: "send_notification";
  recipientIds: string[];
  recipientSources?: import("@/interactiveAutomationWire.generated").InteractiveAutomationRecipientSource[];
  /** Whom its card's chip names — people picked ("특정 사용자") or a person property ("사용자 속성") — kept while
   *  none is chosen yet, as Notion's (seen 2026-11-21); without it the recipients say. */
  recipientKind?: "people" | "property";
  message: string;
  /** The words as one formula, computed as it runs; then the message is empty (interactive_automation_message.go). */
  messageFormula?: string;
  /** The words with mentions in them, as written (and kept beside a formula made from them); the message is their text. */
  messageSpans?: TextSpan[];
}

export interface SendEmailAutomationAction {
  id: string;
  type: "send_email";
  recipientEmail: string;
  subject: string;
  message: string;
  /** The words as one formula, computed as it runs; then the message is empty (interactive_automation_message.go). */
  messageFormula?: string;
  /** The words with mentions in them, as written (and kept beside a formula made from them); the message is their text. */
  messageSpans?: TextSpan[];
}

export interface SendWebhookAutomationAction {
  id: string;
  type: "send_webhook";
  url: string;
  propertyIds: string[];
  headers: Array<{ name: string; value: string }>;
}

export interface SendSlackAutomationAction {
  id: string;
  type: "send_slack";
  connectionId: string;
  channelId: string;
  message: string;
  /** The words as one formula, computed as it runs; then the message is empty (interactive_automation_message.go). */
  messageFormula?: string;
  /** The words with mentions in them, as written (and kept beside a formula made from them); the message is their text. */
  messageSpans?: TextSpan[];
}

export interface ShowConfirmationAutomationAction {
  id: string;
  type: "show_confirmation";
  /** Kept when reading older documents; new confirmations have only a message. */
  title?: string;
  message: string;
  /** The words as one formula, computed as it runs; then the message is empty (interactive_automation_message.go). */
  messageFormula?: string;
  /** The words with mentions in them, as written (and kept beside a formula made from them); the message is their text. */
  messageSpans?: TextSpan[];
  confirmLabel: string;
  cancelLabel: string;
}

export interface OpenPageAutomationAction {
  id: string;
  type: "open_page";
  pageId: string;
  /** "이 페이지": the page the button is on, or the row whose button was pressed; then pageId is "". */
  target?: "trigger_page";
  /** Where it opens: beside the page, over it (none named, Notion's default), or in its place. */
  openIn?: "side_peek" | "center_peek" | "full_page";
}

export interface OpenFormAutomationAction {
  id: string;
  type: "open_form";
  databaseId: string;
  viewId: string;
  /** Where its answer page opens, as a page's (none named: over the page, Notion's default). */
  openIn?: "side_peek" | "center_peek" | "full_page";
}

export interface OpenUrlAutomationAction {
  id: string;
  type: "open_url";
  url: string;
}

/** Runs one of an installed app's actions (Leafen). */
export interface RunAppActionAutomationAction {
  id: string;
  type: "run_app_action";
  appInstallId: string;
  appAction: string;
  appInput: Record<string, unknown>;
}

export type AutomationAction =
  | EditPropertyAutomationAction
  | InsertBlocksAutomationAction
  | AddPageAutomationAction
  | EditPagesAutomationAction
  | ArchivePagesAutomationAction
  | SetPageIconAutomationAction
  | DefineVariablesAutomationAction
  | SendNotificationAutomationAction
  | SendEmailAutomationAction
  | SendWebhookAutomationAction
  | SendSlackAutomationAction
  | ShowConfirmationAutomationAction
  | OpenPageAutomationAction
  | OpenFormAutomationAction
  | OpenUrlAutomationAction
  | RunAppActionAutomationAction;

export interface PageButtonActionDocument {
  version: 1;
  label: string;
  actions: AutomationAction[];
}

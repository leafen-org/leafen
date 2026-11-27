import type { GraphBlockType, GraphViewType } from "@/contentGraph";
import {
  ArrowBendUpRightIcon, AudioBlockIcon, BarChartIcon, BookmarkBlockIcon, BreadcrumbIcon, BulletedListBlockIcon, ButtonBlockIcon, CalloutBlockIcon, ChecklistIcon, ClockIcon, CodeIcon, ColumnsSplitIcon, Copy, CsvFileIcon, DataSourceLinkIcon, DividerBlockIcon, DonutChartIcon, FileText, HeadingFourIcon, HeadingOneIcon, HeadingThreeIcon, HeadingTwoIcon, HorizontalBarChartIcon, iconPixels, ImageBlockIcon, ImportIcon, InlineDatabaseIcon, type LeafenIconSize, LineChartIcon, LinkCaptureIcon, LinkIcon, MarkdownTextIcon, MathBlockIcon, MermaidIcon, NumberChartIcon, NumberedListIcon, OpenInNew, PageLinkIcon, PaperclipBlockIcon, PdfRowIcon, PersonCircleIcon, PresentIcon, QuoteIcon, ServerAppsIcon, ServiceIcon, SmileIcon, SquareRootIcon, SyncedBlockIcon, TableIcon, TableOfContentsIcon, TabsIcon, TextIcon, ToggleHeadingIcon, ToggleListIcon, Trash, VideoBlockIcon, ViewTypeMark, WordDocumentIcon, ZipFileIcon,
} from "@/icons/leafen";
import type { InlineSlashKind, SlashAction, SlashGroup } from "./blockDefinitions";
import { embedService } from "./embedServices";
import { blockColorStyle } from "./blockHandleModel";

type PreservedBlockIconType = GraphBlockType
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
  | "breadcrumb"
  | "table_of_contents"
  | "synced_block"
  | "button"
  | "tab"
  | "column_list"
  | "column"
  | "child_page"
  | "link_to_page"
  | "child_database";

type BlockIconDefinition = {
  id?: string;
  databaseView?: GraphViewType | "list" | "timeline" | "calendar" | "gallery";
  glyph?: string;
  group?: SlashGroup;
  inline?: InlineSlashKind;
  action?: SlashAction;
  colorToken?: string;
  content?: Record<string, unknown>;
  /** An installed app's block. */
  app?: unknown;
  /** "페이지 위치:" of "Turn into". */
  pageIn?: true;
  type: PreservedBlockIconType;
};

type Props = {
  def?: Pick<BlockIconDefinition, "id" | "databaseView" | "glyph" | "group" | "inline" | "action" | "colorToken" | "content" | "app" | "pageIn" | "type">;
  type?: PreservedBlockIconType;
  glyph?: string;
  size?: LeafenIconSize;
};

export function BlockIcon({ def, type, glyph, size = "regular" }: Props) {
  const blockType = type ?? def?.type;
  const fallback = glyph ?? def?.glyph ?? "¶";

  // The inline items take Notion's icons: a person, a page, a clock, a face and √x.
  if (def?.inline === "person") return <PersonCircleIcon size={size} aria-hidden="true" />;
  if (def?.inline === "page") return <FileText artboard size={size} aria-hidden="true" />;
  if (def?.inline === "date") return <ClockIcon size={size} aria-hidden="true" />;
  if (def?.inline === "emoji") return <SmileIcon size={size} aria-hidden="true" />;
  if (def?.inline === "equation") return <SquareRootIcon size={size} aria-hidden="true" />;

  // "페이지 위치:" takes the block menu's icon for it.
  if (def?.pageIn) return <BlockIcon type="link_to_page" size={size} />;

  // An app's block takes the apps' icon.
  if (def?.app) return <ServerAppsIcon size={size} aria-hidden="true" />;

  // The actions take the block menu's icons; a color shows itself.
  if (def?.action === "copy_link") return <LinkIcon size={size} aria-hidden="true" />;
  if (def?.action === "duplicate") return <Copy size={size} aria-hidden="true" />;
  if (def?.action === "move_to") return <ArrowBendUpRightIcon size={size} aria-hidden="true" />;
  if (def?.action === "delete") return <Trash size={size} aria-hidden="true" />;
  if (def?.action === "present") return <PresentIcon size={size} aria-hidden="true" />;
  if (def?.action === "import") {
    // Each format's file, as Notion's "가져오기" items.
    if (def.id === "import_csv") return <CsvFileIcon size={size} aria-hidden="true" />;
    if (def.id === "import_markdown") return <MarkdownTextIcon size={size} aria-hidden="true" />;
    if (def.id === "import_word") return <WordDocumentIcon size={size} aria-hidden="true" />;
    if (def.id === "import_zip") return <ZipFileIcon size={size} aria-hidden="true" />;
    if (def.id === "import_pdf") return <PdfRowIcon size={size} aria-hidden="true" />;
    return <ImportIcon size={size} aria-hidden="true" />;
  }
  if (def?.action === "set_color") {
    return (
      <span aria-hidden="true" style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", width: 20, height: 20, borderRadius: 6,
        boxShadow: "inset 0 0 0 1px color-mix(in srgb, var(--text-default) 11%, transparent)", fontSize: iconPixels(size) * 0.7, fontWeight: 500, lineHeight: 1, ...blockColorStyle(def.colorToken) }}>A</span>
    );
  }

  if (def?.group === "Database" || blockType === "child_database" || blockType === "inline_database") {
    if (def?.id === "chart_bar") return <BarChartIcon size={size} aria-hidden="true" />;
    if (def?.id === "chart_horizontal_bar") return <HorizontalBarChartIcon size={size} aria-hidden="true" />;
    if (def?.id === "chart_line") return <LineChartIcon size={size} aria-hidden="true" />;
    if (def?.id === "chart_donut") return <DonutChartIcon size={size} aria-hidden="true" />;
    if (def?.id === "chart_number") return <NumberChartIcon size={size} aria-hidden="true" />;
    // A view's block takes its layout's mark where every view screen does, as the "/" menu draws it.
    if (def?.databaseView) return <ViewTypeMark type={def.databaseView} blockMenu size={size} />;
    if (def?.id === "inline_database_linked") return <DataSourceLinkIcon size={size} aria-hidden="true" />;
    if (blockType === "inline_database") return <InlineDatabaseIcon size={size} aria-hidden="true" />;
    return <ViewTypeMark type="table" size={size} />;
  }

  // A page takes the page glyph on the menu's 20-unit box, not one filling the row (blockMenuMarks.tsx).
  if (blockType === "child_page") return <FileText artboard size={size} aria-hidden="true" />;
  if (blockType === "link_to_page") return <PageLinkIcon size={size} aria-hidden="true" />;
  if (blockType === "paragraph") return <TextIcon size={size} aria-hidden="true" />;
  if (blockType === "heading_1") return <HeadingOneIcon size={size} aria-hidden="true" />;
  if (blockType === "heading_2") return <HeadingTwoIcon size={size} aria-hidden="true" />;
  if (blockType === "heading_3") return <HeadingThreeIcon size={size} aria-hidden="true" />;
  if (blockType === "heading_4") return <HeadingFourIcon size={size} aria-hidden="true" />;
  if (blockType === "toggle_heading_1") return <ToggleHeadingIcon level={1} size={size} aria-hidden="true" />;
  if (blockType === "toggle_heading_2") return <ToggleHeadingIcon level={2} size={size} aria-hidden="true" />;
  if (blockType === "toggle_heading_3") return <ToggleHeadingIcon level={3} size={size} aria-hidden="true" />;
  if (blockType === "toggle_heading_4") return <ToggleHeadingIcon level={4} size={size} aria-hidden="true" />;
  if (blockType === "to_do") return <ChecklistIcon size={size} aria-hidden="true" />;
  if (blockType === "bulleted_list_item") return <BulletedListBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "numbered_list_item") return <NumberedListIcon size={size} aria-hidden="true" />;
  if (blockType === "toggle") return <ToggleListIcon size={size} aria-hidden="true" />;
  if (blockType === "quote") return <QuoteIcon size={size} aria-hidden="true" />;
  if (blockType === "callout") return <CalloutBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "divider") return <DividerBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "code" && def?.id === "code_mermaid") return <MermaidIcon size={size} aria-hidden="true" />;
  if (blockType === "code") return <CodeIcon size={size} aria-hidden="true" />;
  if (blockType === "equation") return <MathBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "table_of_contents") return <TableOfContentsIcon size={size} aria-hidden="true" />;
  if (blockType === "breadcrumb") return <BreadcrumbIcon size={size} aria-hidden="true" />;
  if (blockType === "synced_block") return <SyncedBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "button") return <ButtonBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "tab") return <TabsIcon size={size} aria-hidden="true" />;
  if (blockType === "column_list") {
    const count = Number(def?.id?.match(/^column_list_(\d)$/)?.[1] ?? 2);
    return <ColumnsSplitIcon count={count} size={size} aria-hidden="true" />;
  }
  if (blockType === "table" || blockType === "table_row") return <TableIcon size={size} aria-hidden="true" />;
  if (blockType === "image") return <ImageBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "video") return <VideoBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "audio") return <AudioBlockIcon size={size} aria-hidden="true" />;
  if (blockType === "bookmark" || blockType === "link_preview") return <BookmarkBlockIcon size={size} aria-hidden="true" />;
  // An embed takes its service's mark, HTML the ↗ (no AI block mark), any other Notion's link in a capture frame.
  if (blockType === "embed") {
    const service = embedService(def?.content?.service);
    if (service) return <ServiceIcon service={service.id} name={service.name} size={size} aria-hidden="true" />;
    return def?.content?.service === "html" ? <OpenInNew size={size} aria-hidden="true" /> : <LinkCaptureIcon size={size} aria-hidden="true" />;
  }
  if (blockType === "pdf") return <PdfRowIcon size={size} aria-hidden="true" />;
  if (blockType === "file") return <PaperclipBlockIcon size={size} aria-hidden="true" />;

  return <>{fallback}</>;
}

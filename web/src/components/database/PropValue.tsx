"use client";

import { pageIconSource, usePageNames } from "@/lib/pageNames";
import { AppPropertyCell } from "@/components/apps/AppPropertyCell";
import { type KeyboardEvent as ReactKeyboardEvent, type MouseEvent as ReactMouseEvent, type ReactNode, type RefObject, memo, useContext, useEffect, useMemo, useRef } from "react";
import { CellCommentsContext } from "./cellCommentsContext";
import { useTranslation } from "react-i18next";
import { filePreviewWidth, useWorkspaceFile, useWorkspaceFileUrl } from "@/lib/fileUrls";
import { useBackgroundImageFailure } from "@/lib/imageLoadFailure";
import { pageHref } from "@/lib/navigation";
import {
  collectPropertyValueSchemaDatabaseIds,
  type PropertyValueSchemaReadOwner,
} from "@/lib/propertyValueReadModel";
import type { DbProperty, FileAttachment, Page } from "@/lib/types";
import { cardShowsPageIcon } from "./CardTitleIcon";
import { chipStyle } from "./colors";
import { formatDateForProperty, formatTimestampForProperty } from "./dateUtils";
import {
  evaluateFormula,
  evaluateRollupCheckboxes,
  evaluateRollupWithComputedDisplay,
  formulaDisplay,
  type FormulaBounds,
} from "./formula";
import { fileAttachmentAddress, isImageAttachment, normalizeFileAttachments } from "./files";
import { FileImagePreview } from "./PropertyFilesCellView";
import { formatNumberForProperty } from "./numberFormat";
import { NumberValueDisplay } from "./NumberValueDisplay";
import { PersonValues } from "./PersonBadge";
import { useRelatedPageOpener } from "./relatedPageOpen";
import { normalizePersonIds } from "./people";
import { rolledUpPageIds, rollupHopDatabaseIds, rollupMayShowCheckboxes, rollupOptionValues, unreadableRelationIds, valueAsIds } from "./rollup";
import { rollupShownText } from "./rollupDateDisplay";
import { rollupShownFunction } from "./rollupPolicy";
import { rollupFormatProperty, rollupNumberShown, rollupNumberText } from "./rollupNumberDisplay";
import { FileBlank, ImageBrokenIcon } from "../icons";
import { hasExplicitPageIcon, PageIconGlyph } from "../page/PageIcon";
import { AttachmentOpenLink } from "./AttachmentOpenLink";
import { propertyLink } from "./propertyLinks";
import type { TitleIconMode } from "./propertyCellTypes";
import {
  placePropertySummary,
  runtimeOwnedPropertySummary,
  verificationPropertySummary,
} from "./structuredPropertyValues";
import { uniqueIdAwaitedAttributes, uniqueIdText } from "./uniqueIdText";
import { mergeStyleModules } from "@/lib/styleModules";
import databaseStyles from "./database.module.css";
import sheet from "./databaseSheet";
import { RichTextCellText } from "./RichTextCellText";
import { rowCellSpans } from "./richTextCellModel";
import { valueAsRead } from "./valueAsRead";
import { shortUrlParts, useUrlDisplay } from "./urlDisplay";
import { untitledPageDisplayTitle } from "../../lib/pageTitle";
import { TitleWithCustomEmoji } from "@/components/page/TitleWithCustomEmoji";
import { RollupPageLinks } from "./RollupPageLinks";
import { useRelationChipsDrawn } from "./relationChipsDrawn";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

// A value looks as its cell does: the title, chips, files, relations and
// checkboxes take the database sheet, and a card's fields the views' sheet
// too, so a card that wraps its properties wraps their values.
const styles: Record<string, string> = mergeStyleModules(sheet, databaseStyles);

const EMPTY_PAGES: Record<string, Page> = {};
const EMPTY_PROPERTIES: DbProperty[] = [];


export type PropertyValueReadContext = {
  bounds: FormulaBounds;
  contextIdentity: string;
  hydratedRelationTargetIds: ReadonlySet<string>;
  pagesById: Readonly<Record<string, Page>>;
  propsByDatabaseId: Readonly<Record<string, readonly DbProperty[]>>;
  schemaReadOwner: PropertyValueSchemaReadOwner;
  snapshotIdentity: string;
  userId?: string;
};

type PropValueProps = Parameters<typeof LeafenPropValue>[0];

type RelationChipWays = {
  click(pageId: string, event: ReactMouseEvent<HTMLAnchorElement>): void;
  line(pageId: string, event: ReactMouseEvent<HTMLElement> | ReactKeyboardEvent<HTMLElement>): void;
};

/**
 * A related page's chip, for those who only read it: drawn again for its page
 * and how it opens alone, its presses going by the value as last drawn
 * (`ways`). A row's page holding a relation of 100 pages drew all their
 * chips again each time it was drawn (seen 2026-11-13), as an editor's did
 * before (PropertyRelationCellView).
 */
const ReadonlyRelationChip = memo(function ReadonlyRelationChip({ page, href, line, link, ways }: {
  page: Page; href: string; line: boolean; link: boolean; ways: RefObject<RelationChipWays>;
}) {
  const pageNames = usePageNames();
  const { t } = useTranslation("propValue");
  const title = pageNames.title(page);
  return (
    <span className={styles.relationChip} data-row-relation-chip
      {...(line ? { role: "button", tabIndex: 0, "aria-label": t("openRelation", { title }),
        onClick: (event: ReactMouseEvent<HTMLElement>) => ways.current.line(page.id, event), onKeyDown: (event: ReactKeyboardEvent<HTMLElement>) => { if (fieldEnter(event) || event.key === " ") ways.current.line(page.id, event); } } : {})}>
      <span className={styles.relationPageIcon} aria-hidden="true">
        <PageIconGlyph page={pageIconSource(page)} size={18} />
      </span>
      {link ? (
        <a
          className={styles.relationChipLink}
          href={href}
          aria-label={t("openRelation", { title })}
          onClick={(e) => ways.current.click(page.id, e)}
          onAuxClick={(e) => e.stopPropagation()}
        >
          <span className={styles.relationChipName}><TitleWithCustomEmoji text={title} named={page} workspaceId={page.workspaceId} /></span>
        </a>
      ) : (
        <span className={styles.relationChipText}><span className={styles.relationChipName}>
          <TitleWithCustomEmoji text={title} named={page} workspaceId={page.workspaceId} /></span></span>
      )}
    </span>
  );
});

/** How much room a card's discussion mark leaves round a value of each type (PropValue). */
const MARK_ROOMS: Readonly<Record<string, string>> = { person: "chips", relation: "chips", select: "options", status: "options", multi_select: "options" };

/** Read-only display of a row's value for a property (used by list/gallery/board); an app's cell where an app draws it. */
export function PropValue(props: PropValueProps) {
  // A value with an open discussion is marked on a card as in its table's cell (Notion's, seen 2026-11-03).
  const discussed = useContext(CellCommentsContext)?.discussed(props.row.id, props.prop.id);
  const value = !props.prop.config?.appEditor ? <LeafenPropValue {...props} />
    : <AppPropertyCell row={props.row} prop={props.prop} editable={false} onSet={() => undefined} fallback={<LeafenPropValue {...props} />} />;
  // One mark round the whole value, as Notion's card (seen 2026-11-11): 4px round people and pages, 2px round options.
  const room = MARK_ROOMS[props.prop.type];
  return discussed && props.presentation !== "rowDetail"
    ? <span className={styles.discussedValue} data-property-discussed="true" data-mark-room={room}>{value}</span> : value;
}

function LeafenPropValue({
  context,
  row,
  prop,
  interactive = true,
  linksOpen = interactive,
  onOpenPage,
  pageHrefForRelation,
  presentation = "default",
  titleIcon = "own",
  checkboxName = false,
}: {
  context: PropertyValueReadContext;
  row: Page;
  prop: DbProperty;
  interactive?: boolean;
  /** Whether a URL, email or phone value opens as a link, as on a board's or gallery's card where nothing else does. */
  linksOpen?: boolean;
  onOpenPage?: (pageId: string) => void;
  pageHrefForRelation?: (pageId: string) => string;
  presentation?: "default" | "rowDetail";
  titleIcon?: TitleIconMode;
  /** A card whose values stack names its checkbox beside it, as Notion's board, gallery and calendar cards (seen 2026-11-05). */
  checkboxName?: boolean;
}) {
  const { t } = useTranslation(["propValue", "common", "tableView", "topBar"]);
  const urlDisplay = useUrlDisplay(prop);
  const openRelated = useRelatedPageOpener();
  // What a related page's chip does, as this value is now drawn (ReadonlyRelationChip).
  const relationWays = useRef<RelationChipWays>(null!);
  // A long relation draws its first pages as it shows and the rest after (relationChipsDrawn.ts).
  const drawnChips = useRelationChipsDrawn(prop.type === "relation" ? valueAsIds(valueAsRead(row, prop), context.bounds.rollup).length : 0);
  const needsPages = prop.type === "relation" || prop.type === "rollup" || prop.type === "formula";
  const pagesById = (needsPages ? context.pagesById : EMPTY_PAGES) as Record<string, Page>;
  const propsByDb = context.propsByDatabaseId as Record<string, DbProperty[]>;
  const sourceProps = (context.propsByDatabaseId[prop.databaseId] ?? EMPTY_PROPERTIES) as DbProperty[];
  const relationTargetDbId =
    prop.type === "relation" ? prop.config?.relationDatabaseId ?? prop.databaseId : undefined;
  const relationProp = sourceProps.find(
    (item) => item.id === prop.config?.rollupRelationPropertyId
  );
  const targetDbId = relationProp?.config?.relationDatabaseId ?? relationProp?.databaseId;
  const targetProps = (targetDbId
    ? context.propsByDatabaseId[targetDbId] ?? EMPTY_PROPERTIES
    : EMPTY_PROPERTIES) as DbProperty[];
  const v = valueAsRead(row, prop);

  // Every database a rollup of rollups reaches, each known once the one
  // before it loads.
  const hopDbIds =
    prop.type === "rollup"
      ? rollupHopDatabaseIds(prop, targetProps, propsByDb, context.bounds.rollup).join("\u0000")
      : "";
  const requiredSchemaDatabaseIds = useMemo(
    () => collectPropertyValueSchemaDatabaseIds([
      relationTargetDbId,
      prop.type === "rollup" ? targetDbId : undefined,
      ...(hopDbIds ? hopDbIds.split("\u0000") : []),
    ]),
    [prop.type, relationTargetDbId, hopDbIds, targetDbId],
  );

  useEffect(() => {
    context.schemaReadOwner.request(requiredSchemaDatabaseIds);
  }, [
    context.contextIdentity,
    context.schemaReadOwner,
    context.snapshotIdentity,
    requiredSchemaDatabaseIds,
  ]);
  const personValueBounds = {
    maxNodes: context.bounds.rollup.maxPersonNodes,
    maxDepth: context.bounds.rollup.maxPersonDepth,
    maxEntries: context.bounds.rollup.maxPeopleEntries,
  };

  // A page's property list shows a plain value in the page's 14px words,
  // not a card's 12px grey (2026-10-24: a number on a reader's page and in
  // the layout builder's preview).
  const rowDetail = presentation === "rowDetail";
  const field = rowDetail ? `${styles.cardField} ${styles.rowPropertyFieldValue}` : styles.cardField;

  if (prop.type === "title") {
    // A table's untitled row keeps its icon only when it has one of its own or content (PropertyTextCell).
    const showsIcon = titleIcon === "page" ? Boolean(row.title) || cardShowsPageIcon(row)
      : titleIcon === "own" && hasExplicitPageIcon(pageIconSource(row));
    const titleSpans = rowCellSpans(row, prop.id, row.title);
    return row.title || (titleIcon === "page" && showsIcon) ? (
      <span className={`${styles.cardField} ${styles.titleValue}`}>
        {showsIcon && (
          <span className={styles.titleValueIcon} aria-hidden="true" data-title-icon={titleIcon}>
            <PageIconGlyph page={pageIconSource(row)} size={16} fallback={titleIcon === "page" ? "file" : "none"} />
          </span>
        )}
        {row.title ? <span className={styles.titleValueText} data-search-text>{titleSpans
          ? <RichTextCellText spans={titleSpans} interactive={interactive} /> : row.title}</span> : null}
      </span>
    ) : null;
  }

  if (prop.type === "select" || prop.type === "multi_select" || prop.type === "status") {
    const opts = prop.config?.options ?? [];
    const ids = Array.from(new Set(
      (Array.isArray(v) ? v : v ? [v] : []).map(String)
    ));
    if (ids.length === 0) return null;
    return (
      <>
        {ids.map((id) => {
          const o = opts.find((x) => x.id === id);
          return o ? (
            <span key={id} className={styles.chip} style={chipStyle(o.color)} data-status-chip={prop.type === "status" ? "" : undefined}>
              <span className={styles.chipText}>{o.name}</span>
            </span>
          ) : null;
        })}
      </>
    );
  }

  if (prop.type === "checkbox") {
    return (
      <span className={styles.cardField} data-checkbox-field>
        <span
          className={styles.cardCheckbox}
          data-checked={v ? "true" : undefined}
          aria-label={v ? t("propValue:checkbox.checked") : t("propValue:checkbox.unchecked")}
        >
        </span>
        {checkboxName ? <span className={styles.cardCheckboxName}>{prop.name}</span> : null}
      </span>
    );
  }
  if (prop.type === "number") {
    const property = { config: (prop.config ?? {}) as unknown as Record<string, unknown> };
    return formatNumberForProperty(v, property)
      ? <span className={field}><NumberValueDisplay value={v} property={property} /></span> : null;
  }
  if (prop.type === "relation") {
    const relationIds = valueAsIds(v, context.bounds.rollup);
    const pages = relationIds
      .map((id) => pagesById[id])
      .filter((page): page is Page => !!page && !page.inTrash);
    const loadedPageIds = new Set(pages.map((page) => page.id));
    const unresolvedIds = relationIds.filter((id) => !loadedPageIds.has(id));
    const pendingCount = unresolvedIds.filter(
      (id) => !context.hydratedRelationTargetIds.has(id),
    ).length;
    const missingCount = unresolvedIds.length - pendingCount;
    const unreadable = unreadableRelationIds(relationIds, pagesById, context.hydratedRelationTargetIds).length > 0;
    if (pages.length === 0 && pendingCount === 0 && missingCount === 0) return null;
    relationWays.current = {
      click(pageId, e) {
        e.stopPropagation();
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
        e.preventDefault();
        onOpenPage?.(pageId);
      },
      line(pageId, event) {
        event.stopPropagation();
        event.preventDefault();
        if (event.metaKey || event.ctrlKey) window.open(pageHref(pageId), "_blank", "noopener");
        else openRelated(pageId);
      },
    };
    // On a row's page a page is a line pressed as one, for those who only read it too (relatedPageOpen.ts).
    const pressedLine = presentation === "rowDetail" && !(interactive && onOpenPage);
    const link = Boolean(interactive && onOpenPage);
    return (
      <>
        <span className={styles.relationChips} data-readonly="true">
          {pages.map((page, index) => index >= drawnChips ? null : (
            <ReadonlyRelationChip key={page.id} page={page} href={pageHrefForRelation?.(page.id) ?? pageHref(page.id)}
              line={pressedLine} link={link} ways={relationWays} />
          ))}
          {/* The pages after the first, as a compact card counts them (DatabaseCardValues.module.css). */}
          {pages.length > 1 ? <span className={styles.relationMore} data-row-relation-more>+{pages.length - 1}</span> : null}
          {pendingCount > 0 && (
            <span
              className={styles.relationChipSkeleton}
              data-row-relation-loading
              aria-label={t("propValue:relationLoading")}
            />
          )}
          {unreadable ? (
            <span className={styles.relationMissingChip} data-row-relation-missing="no-access">{t("propValue:noAccess")}</span>
          ) : missingCount > 0 && (
            <span
              className={styles.relationMissingChip}
              data-row-relation-missing
              aria-label={t("propValue:relationUnavailable")}
            >
              {t("propValue:unavailable")}
            </span>
          )}
        </span>
      </>
    );
  }
  if (prop.type === "files") {
    const files = normalizeFileAttachments(v);
    if (files.length === 0) return null;
    return (
      <span className={styles.filesChips} data-readonly="true">
        {files.map((file) => (
          <ReadonlyFileChip key={file.id} file={file} />
        ))}
      </span>
    );
  }
  if (prop.type === "rollup") {
    if (prop.config?.rollupRelationPropertyId && !relationProp) {
      return (
        <span
          className={styles.cardField}
          data-rollup-broken="relation"
          aria-label={t("propValue:unavailable")}
        >
          {t("propValue:unavailable")}
        </span>
      );
    }
    if (relationProp && unreadableRelationIds(valueAsIds(row.properties?.[relationProp.id], context.bounds.rollup),
      pagesById, context.hydratedRelationTargetIds).length > 0) {
      return <span className={styles.cardField} data-rollup-no-access>{t("propValue:noAccess")}</span>;
    }
    const target = targetProps.find((property) => property.id === prop.config?.rollupTargetPropertyId);
    // Pages shown as they are are those pages, as a relation's on a card
    // (Notion's, seen 2026-11-12: "셋째" and "첫 관련 page 이름" with their
    // icons, not their names as words), each a link to its page where a
    // value's links open (RollupPageLinks); until each is loaded, words.
    const rolledFunction = rollupShownFunction(prop.config) ?? "show_original";
    const rolledIds = relationProp && (rolledFunction === "show_original" || rolledFunction === "show_unique")
      ? rolledUpPageIds(row, relationProp, target, pagesById, context.bounds.rollup) : null;
    const rolledPages = (rolledIds ? rolledFunction === "show_unique" ? [...new Set(rolledIds)] : rolledIds : [])
      .flatMap((id) => pagesById[id] ? [pagesById[id]] : []);
    if (rolledIds?.length && rolledPages.length === rolledIds.length) return (
      <RollupPageLinks pages={rolledPages} readonly asText={!linksOpen && !onOpenPage && !rowDetail}
        onOpenPage={onOpenPage} hrefOf={pageHrefForRelation} />
    );
    const chips = rollupOptionValues(row, prop, relationProp, target, pagesById, context.bounds.rollup);
    if (chips) return <>{chips.map(({ key, option, status }) => (
      <span key={key} className={styles.chip} style={chipStyle(option.color)} data-status-chip={status ? "" : undefined} data-rollup-option>
        <span className={styles.chipText}>{option.name}</span></span>
    ))}</>;
    // Checkboxes shown as they are are their boxes, as Notion's (seen 2026-11-12).
    const checks = rollupMayShowCheckboxes(targetProps, prop) ? evaluateRollupCheckboxes({
      row, prop, sourceProps, targetProps, pagesById, propsByDb, bounds: context.bounds,
    }) : null;
    // A share it doesn't count is empty on a card, as Notion's leaves it off
    // (seen 2026-11-12), though a table and a page show its boxes.
    const shownFunction = rollupShownFunction(prop.config);
    if (checks && !rowDetail && (shownFunction === "percent_empty" || shownFunction === "percent_not_empty")) return null;
    if (checks) return <span className={styles.cardField} data-rollup-checks>{checks.map((checked, index) => (
      <span key={index} className={styles.cardCheckbox} data-checked={checked ? "true" : undefined}
        aria-label={checked ? t("propValue:checkbox.checked") : t("propValue:checkbox.unchecked")}>
      </span>
    ))}</span>;
    const text = rollupShownText(prop.config?.rollupFunction, evaluateRollupWithComputedDisplay({
      row,
      prop,
      sourceProps,
      targetProps,
      pagesById,
      propsByDb,
      bounds: context.bounds,
    }));
    const targetProperty = targetProps.find(property => property.id === prop.config?.rollupTargetPropertyId);
    const presentation = targetProperty ? rollupFormatProperty(targetProperty, targetProps, propsByDb) : undefined;
    const value = rollupNumberText(prop.config?.rollupFunction, text, presentation, context.bounds.rollup);
    const shown = rollupNumberShown(prop, text, presentation);
    if (shown) return <span className={field}><NumberValueDisplay value={shown.value} property={{ config: shown.config }} /></span>;
    return value ? <span className={field}>{value}</span> : null;
  }
  if (prop.type === "formula") {
    const display = formulaDisplay(row, prop, context.bounds, untitledPageDisplayTitle("page"), () => evaluateFormula({
      row,
      prop,
      props: sourceProps,
      pagesById,
      propsByDb,
      bounds: context.bounds,
    }));
    if (display.checked !== undefined) {
      return (
        <span className={styles.cardField}>
          <span
            className={styles.cardCheckbox}
            data-checked={display.checked ? "true" : undefined}
            aria-label={display.checked ? t("propValue:checkbox.checked") : t("propValue:checkbox.unchecked")}
          >
          </span>
        </span>
      );
    }
    // A list of true or false alone, a box each, as a rollup's (Notion's, seen 2026-11-12).
    if (display.checks) return <span className={styles.cardField} data-formula-checks>{display.checks.map((checked, index) => (
      <span key={index} className={styles.cardCheckbox} data-checked={checked ? "true" : undefined}
        aria-label={checked ? t("propValue:checkbox.checked") : t("propValue:checkbox.unchecked")}>
      </span>
    ))}</span>;
    // People alone show as a person property's, pages alone as a relation's (Notion's, seen 2026-11-12).
    if (display.people?.length) return <PersonValues ids={display.people} currentUserId={context.userId} />;
    const formulaPages = (display.pages ?? []).flatMap((id) => pagesById[id] ? [pagesById[id]] : []);
    if (display.pages?.length && formulaPages.length === display.pages.length) return (
      <RollupPageLinks pages={formulaPages} readonly asText={!linksOpen && !onOpenPage && !rowDetail}
        onOpenPage={onOpenPage} hrefOf={pageHrefForRelation} />
    );
    if (!display.text) return null;
    return (
      <span className={field}>
        {display.number !== undefined ? <NumberValueDisplay value={display.number} property={{ config: (prop.config ?? {}) as Record<string, unknown> }} />
          : display.spans ? <RichTextCellText spans={display.spans} interactive={interactive} /> : display.text}
      </span>
    );
  }
  if (prop.type === "date") {
    const text = formatDateForProperty(v, prop);
    return text ? <span className={field}>{text}</span> : null;
  }
  if (prop.type === "person") {
    const people = Array.from(new Set(normalizePersonIds(v, personValueBounds)));
    if (people.length === 0) return null;
    return <PersonValues ids={people} currentUserId={context.userId} />;
  }
  if (prop.type === "created_time") {
    return row.createdAt ? (
      <span className={styles.cardField}>{formatTimestampForProperty(row.createdAt, prop)}</span>
    ) : null;
  }
  if (prop.type === "last_edited_time") {
    return row.updatedAt ? (
      <span className={styles.cardField}>{formatTimestampForProperty(row.updatedAt, prop)}</span>
    ) : null;
  }
  if (prop.type === "created_by" || prop.type === "last_edited_by") {
    const people = normalizePersonIds(
      prop.type === "created_by" ? row.createdBy : row.lastEditedBy,
      personValueBounds,
    );
    if (people.length === 0) return null;
    return <PersonValues ids={people.slice(0, 1)} currentUserId={context.userId} />;
  }
  if (prop.type === "url" || prop.type === "email" || prop.type === "phone") {
    const text = v == null ? "" : String(v).trim();
    if (!text) return null;
    const link = propertyLink(prop.type, text);
    // A URL short reads its address and, dimmed, what follows (urlDisplay.ts).
    const { host, path } = prop.type === "url" && !urlDisplay.full ? shortUrlParts(text) : { host: text, path: "" };
    const shown = host + path;
    const words = path ? <>{host}<span className={styles.urlPath}>{path}</span></> : shown;
    // The value is the link, inline over a faint line, as a table's; on a card
    // it opens the link, not the card (seen 2026-11-03 on a gallery card). A
    // URL that holds no link reads as plain text (propertyLinks.ts).
    return (
      <span className={`${styles.cardField} ${styles.cardLinkField}`} title={shown !== text ? text : undefined}>
        {linksOpen && link?.href ? (
          <a
            className={styles.cardLink}
            href={link.href}
            target={prop.type === "url" ? "_blank" : undefined}
            rel="noreferrer noopener"
            onClick={(e) => e.stopPropagation()}
            onAuxClick={(e) => e.stopPropagation()}
          >
            {words}
          </a>
        ) : <span className={link ? styles.cardLink : undefined}>{words}</span>}
      </span>
    );
  }

  if (prop.type === "rich_text") {
    const text = v == null ? "" : String(v);
    if (!text) return null;
    const className =
      presentation === "rowDetail"
        ? `${styles.cardField} ${styles.rowPropertyTextValue}`
        : styles.cardField;
    const spans = rowCellSpans(row, prop.id, text);
    return (
      <span className={className} data-row-property-text={presentation === "rowDetail" ? "true" : undefined}>
        {spans ? <RichTextCellText spans={spans} interactive={interactive} /> : text}
      </span>
    );
  }

  if (prop.type === "button") {
    // A database button shows its property's name, as in Notion.
    const label = prop.name;
    return label ? (
      <span className={styles.cardField} data-structured-property="button">{label}</span>
    ) : null;
  }

  if (prop.type === "unique_id") {
    return <span className={`${styles.cardField} ${styles.uniqueIdValue}`} {...uniqueIdAwaitedAttributes(prop, v)}>{uniqueIdText(prop, v)}</span>;
  }

  if (prop.type === "place") {
    const text = placePropertySummary(v);
    return text ? (
      <span className={styles.cardField} data-structured-property="place">{text}</span>
    ) : null;
  }

  if (prop.type === "verification") {
    const text = verificationPropertySummary(v, {
      verified: t("tableView:verified"),
      expired: t("topBar:expired"),
      verifiedUntil: (date) => t("tableView:verifiedUntil", { date }),
    });
    return text ? (
      <span className={styles.cardField} data-structured-property="verification">{text}</span>
    ) : null;
  }

  if (prop.type === "location" || prop.type === "last_visited_time") {
    const text = runtimeOwnedPropertySummary(prop.type, v);
    return text ? (
      <span className={styles.cardField} data-structured-property={prop.type}>{text}</span>
    ) : null;
  }

  if (v == null || v === "") return null;
  return <span className={styles.cardField}>{String(v)}</span>;
}

function ReadonlyFileChip({
  file,
}: {
  file: FileAttachment;
}) {
  const url = useWorkspaceFileUrl(fileAttachmentAddress(file), ["data:image/"]);
  // The thumbnail is a small preview; the link opens the original.
  const thumb = useWorkspaceFile(fileAttachmentAddress(file), ["data:image/"], { width: filePreviewWidth(120) });
  if (isImageAttachment(file)) {
    return (
      <span className={styles.fileImageChip}>
        <ReadonlyFileLink file={file} href={url}><FileImagePreview file={file} /></ReadonlyFileLink>
      </span>
    );
  }
  return (
    <span className={styles.fileChip}>
      <ReadonlyFileLink file={file} href={url}>
        <ReadonlyFileThumb file={file} url={thumb.url} refused={thumb.failedStatus} />
        <span className={styles.fileName}>{file.name}</span>
      </ReadonlyFileLink>
    </span>
  );
}

function ReadonlyFileLink({
  file,
  href,
  children,
}: {
  file: FileAttachment;
  href: string;
  children: ReactNode;
}) {
  const { t } = useTranslation(["propValue", "common"]);

  return (
    <AttachmentOpenLink
      file={file}
      href={href}
      className={styles.fileChipLink}
      ariaLabel={t("propValue:openOrDownloadFile", { name: file.name })}
    >
      {children}
    </AttachmentOpenLink>
  );
}

function ReadonlyFileThumb({ file, url, refused }: { file: FileAttachment; url: string; refused: number }) {
  const isImage = isImageAttachment(file);
  // A picture that couldn't be loaded shows a broken picture and any other file a blank page, as the cell's do.
  const failed = useBackgroundImageFailure(isImage ? url : undefined, isImage ? refused : 0);
  const style = isImage && url && !failed ? { backgroundImage: `url("${url.replace(/"/g, '\\"')}")` } : undefined;

  return (
    <span className={styles.fileThumb} data-image={isImage ? "true" : undefined} data-image-failed={failed ? "true" : undefined} style={style}>
      {!isImage && <FileBlank size="regular" aria-hidden="true" />}
      {failed && <ImageBrokenIcon size="regular" aria-hidden="true" />}
    </span>
  );
}

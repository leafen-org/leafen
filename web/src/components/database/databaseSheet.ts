import { mergeStyleModules } from "@/lib/styleModules";
import viewTabs from "./DatabaseViewTabs.module.css";
import viewConfiguration from "./DatabaseViewConfiguration.module.css";
import toolbar from "./DatabaseToolbar.module.css";
import templateMenu from "./DatabaseTemplateMenu.module.css";
import templateSettings from "./DatabaseTemplateSettings.module.css";
import automation from "./DatabaseAutomation.module.css";
import layout from "./DatabaseLayout.module.css";
import settingValueRow from "./DatabaseSettingValueRow.module.css";
import layoutEditChart from "./DatabaseLayoutEditChart.module.css";
import sourceProperties from "./DatabaseSourceProperties.module.css";
import rules from "./DatabaseRules.module.css";
import tableChrome from "./DatabaseTableChrome.module.css";
import tableBoxBand from "./DatabaseTableBoxBand.module.css";
import propertyHeader from "./DatabasePropertyHeader.module.css";
import propertyOptions from "./DatabasePropertyOptions.module.css";
import tableGroups from "./DatabaseTableGroups.module.css";
import tableRows from "./DatabaseTableRows.module.css";
import tableWindowedRows from "./DatabaseTableWindowedRows.module.css";
import tablePhoneCell from "./DatabaseTablePhoneCell.module.css";
import tableSelection from "./DatabaseTableSelection.module.css";
import cellBasics from "./DatabaseCellBasics.module.css";
import peopleDateCells from "./DatabasePeopleDateCells.module.css";
import fileCells from "./DatabaseFileCells.module.css";
import relationCells from "./DatabaseRelationCells.module.css";
import relationSearch from "./DatabaseRelationSearch.module.css";
import selectCells from "./DatabaseSelectCells.module.css";
import boardGroups from "./DatabaseBoardGroups.module.css";
import boardCards from "./DatabaseBoardCards.module.css";
import cardValues from "./DatabaseCardValues.module.css";
import secondaryViews from "./DatabaseSecondaryViews.module.css";
import cardTitleBox from "./DatabaseCardTitleBox.module.css";
import phoneList from "./DatabasePhoneList.module.css";
import phoneCards from "./DatabasePhoneCards.module.css";
import calendar from "./DatabaseCalendar.module.css";
import timelineCore from "./DatabaseTimelineCore.module.css";
import timelineOffRange from "./DatabaseTimelineOffRange.module.css";
import timelineTable from "./DatabaseTimelineTable.module.css";
import timelineControls from "./DatabaseTimelineControls.module.css";
import rowPreview from "./DatabaseRowPreview.module.css";
import rowPeekBody from "./DatabaseRowPeekBody.module.css";
import rowProperties from "./DatabaseRowProperties.module.css";
import headToolsZone from "./DatabaseHeadToolsZone.module.css";
import tableHeadGround from "./DatabaseTableHeadGround.module.css";

/**
 * The database views' stylesheet: one sheet, kept as these modules in its
 * order. Its rules name classes across the modules — a table cell's chips, a
 * row's open button shown on hover, a view's empty state — so every
 * database view takes a class name from all of them at once, and the modules
 * are imported here only, in this order, which is the cascade's.
 */
const databaseSheet = mergeStyleModules(
  viewTabs, viewConfiguration, toolbar, templateMenu, templateSettings, automation, layout, settingValueRow, layoutEditChart, sourceProperties, rules,
  tableChrome, propertyHeader, propertyOptions, tableGroups, tableRows, tableWindowedRows, tablePhoneCell, tableSelection, cellBasics, peopleDateCells, fileCells,
  relationCells, relationSearch, selectCells, boardGroups, boardCards, cardValues, secondaryViews, cardTitleBox, phoneList, phoneCards, calendar, timelineCore, timelineOffRange, timelineTable, timelineControls,
  rowPreview, rowPeekBody, rowProperties, tableBoxBand, headToolsZone, tableHeadGround,
);

export default databaseSheet;

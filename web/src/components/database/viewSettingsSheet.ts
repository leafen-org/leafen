import { mergeStyleModules } from "@/lib/styleModules";
import panel from "./DatabaseViewSettingsPanel.module.css";
import settingValueRow from "./DatabaseSettingValueRow.module.css";

/** The view settings panel's sheet: its own module and the settings rows' name-and-value rule it shares with the
 *  settings screens' choice rows (databaseSheet), in this order. */
const viewSettingsSheet = mergeStyleModules(panel, settingValueRow);

export default viewSettingsSheet;

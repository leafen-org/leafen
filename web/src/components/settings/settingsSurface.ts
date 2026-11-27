import { createContext } from "react";

/**
 * Whether a control stands in the settings window — its sections and the
 * windows they open. Its choices on a touch screen rise on the sheet a
 * settings row's choices take there (AppSelect, SettingsTouchChoice), as the
 * phone's settings lists do, whatever look its button has.
 */
export const InSettingsContext = createContext(false);

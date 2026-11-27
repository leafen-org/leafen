import connectionStyles from "./NotionTokenGuideConnection.module.css";
import developerStyles from "./NotionTokenGuideDeveloper.module.css";
import leafenStyles from "./NotionTokenGuideLeafen.module.css";

// The Notion token guide's screens are styled in three files (Notion's app
// and its Developer connections, the connection's page, Leafen's window);
// scenes use one map of their class names.
const files = [developerStyles, connectionStyles, leafenStyles];
const keys = new Set(files.flatMap((file) => Object.keys(file)));

export const styles = Object.fromEntries(
  [...keys].map((key) => [key, files.map((file) => file[key]).filter(Boolean).join(" ")]),
) as Record<string, string>;

// Imported first by main.tsx, so the page's fetch is bounded before any
// module keeps a reference to it (lib/requestDeadline.ts).
import { installFetchDeadline } from "./requestDeadline";

installFetchDeadline();

import { createContext, useContext, type ReactNode } from "react";
import { createPortal } from "react-dom";

/** A view owns its projected-row controls; the shared toolbar only hosts them. */
export const DatabaseViewControlsContext = createContext<HTMLElement | null>(null);
export function DatabaseViewControls({ children }: { children: ReactNode }) {
  const slot = useContext(DatabaseViewControlsContext);
  return slot ? createPortal(children, slot) : children;
}

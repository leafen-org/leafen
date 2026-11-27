import { DeviceMobile as PhosphorDeviceMobile, Laptop as PhosphorLaptop, Monitor as PhosphorMonitor, Moon as PhosphorMoon, QrCode as PhosphorQrCode, Sun as PhosphorSun } from "@phosphor-icons/react";
import { PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

// A signed-in device in the account's device table, as Notion's (seen
// 2026-10-12): a laptop for a computer, a phone for a phone or tablet.
export const LaptopIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorLaptop} />
);

export const DeviceMobileIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorDeviceMobile} />
);

// A desktop computer's screen: what a touch screen's form view points to (Notion's, seen 2026-11-13).
export const MonitorIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorMonitor} />
);

// Scanning a server's QR code to connect (the phone app's first start).
export const QrCodeIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorQrCode} />
);

// A published site's theme tiles, as Notion's site editor (seen 2026-11-06):
// a laptop for the system's, a sun for light mode, a moon for dark mode.
export const SunIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorSun} />
);

export const MoonIcon = (p: LeafenIconProps) => (
  <PhosphorIconSvg {...p} icon={PhosphorMoon} />
);

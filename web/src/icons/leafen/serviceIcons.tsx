import { CodepenLogo, EnvelopeSimple, FacebookLogo, LinkedinLogo, SlackLogo, WhatsappLogo, XLogo } from "@phosphor-icons/react";
import { IconSvg, PhosphorIconSvg, type LeafenIconProps } from "./iconPrimitives";

/**
 * Service marks share a 20-unit rounded frame and the surrounding text color.
 * Only reviewed monochrome logos belong here; every other service uses its
 * name's initial. Menus and the service's empty state share this policy.
 */
// A row sharing to a service shows its mark alone; a framed initial reads as a placeholder.
const BARE_MARKS: Record<string, typeof XLogo> = {
  x: XLogo, linkedin: LinkedinLogo, facebook: FacebookLogo, whatsapp: WhatsappLogo, email: EnvelopeSimple,
};

export function ServiceIcon({
  service,
  name,
  ...props
}: LeafenIconProps & { service: string; name: string }) {
  const mark = BARE_MARKS[service];
  if (mark) {
    return (
      <IconSvg {...props} viewBox="0 0 20 20" glyph={`service-${service}`}>
        <PhosphorIconSvg icon={mark} size="small" x="2" y="2" stroke="none" />
      </IconSvg>
    );
  }
  return (
    <IconSvg {...props} viewBox="0 0 20 20" glyph={`service-${service}`}>
      <rect x="0.5" y="0.5" width="19" height="19" rx="2" strokeWidth="1" strokeOpacity="0.35" />
      {service === "codepen" || service === "slack" ? (
        <PhosphorIconSvg icon={service === "slack" ? SlackLogo : CodepenLogo} size="small" x="2" y="2" stroke="none" />
      ) : (
        <text x="10" y="10" textAnchor="middle" dominantBaseline="central"
          fill="currentColor" stroke="none" fontSize="12" fontWeight="600">
          {Array.from(name.trim())[0]?.toUpperCase() ?? "?"}
        </text>
      )}
    </IconSvg>
  );
}

import type { LucideIcon } from "lucide-react";

/**
 * A status with an icon and words, so the state is never shown by color alone
 * (Requirement 13.10). The text is 20 pixels, above the 18 pixel minimum.
 */
export function StatusLine({ icon: Icon, text }: { icon: LucideIcon; text: string }) {
  return (
    <p className="flex items-center gap-3 text-xl font-semibold">
      <Icon aria-hidden="true" className="h-7 w-7 shrink-0" />
      <span>{text}</span>
    </p>
  );
}

import { Info } from "lucide-react";
import type { ReactNode } from "react";

import { NOTICE_BOX } from "./ui";

/**
 * A calm message in the page. It stays until the state changes or the person
 * acts, and it never disappears by itself (Requirement 13.8). Extra content,
 * such as a button, goes in `children`.
 */
export function NoticeBlock({ message, children }: { message: string; children?: ReactNode }) {
  return (
    <div role="status" className={NOTICE_BOX}>
      <p className="flex items-start gap-3 text-lg font-semibold">
        <Info aria-hidden="true" className="mt-0.5 h-6 w-6 shrink-0 text-amber-ink" />
        <span>{message}</span>
      </p>
      {children}
    </div>
  );
}

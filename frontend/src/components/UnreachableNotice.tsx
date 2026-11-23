import { WifiOff } from "lucide-react";

import { COPY } from "../lib/copy";
import { BUTTON_PRIMARY, NOTICE_BOX } from "./ui";

/**
 * Shown when the server cannot be reached. It says the money has not moved and
 * offers one button that repeats what failed. It stays until a retry works or a
 * new payment starts (Requirement 9.3).
 */
export function UnreachableNotice({ onRetry }: { onRetry: () => void }) {
  return (
    <div role="status" className={NOTICE_BOX}>
      <p className="flex items-start gap-3 text-lg font-semibold">
        <WifiOff aria-hidden="true" className="mt-0.5 h-6 w-6 shrink-0 text-amber-ink" />
        <span>{COPY.unreachable.message}</span>
      </p>
      <button type="button" onClick={onRetry} className={BUTTON_PRIMARY}>
        {COPY.unreachable.retry}
      </button>
    </div>
  );
}

import { Clock } from "lucide-react";

import type { VaultView } from "../lib/api";
import { COPY } from "../lib/copy";
import { useCountdown } from "../lib/countdown";

/**
 * The time left on a locked vault, as `mm:ss` in a `role="timer"` element. Its
 * accessible label says the time in words and changes at most every 10 seconds.
 * White on deep teal is 9.5:1, above the 7:1 minimum. `onZero` runs once when
 * the count reaches 00:00.
 */
export function Countdown({ view, onZero }: { view: VaultView; onZero: () => void }) {
  const { text, spoken } = useCountdown(view, onZero);
  return (
    <div
      role="timer"
      aria-label={`${COPY.vaultCard.timeLeft}: ${spoken}`}
      className="inline-flex w-fit items-center gap-3 rounded-xl bg-vault-deep px-4 py-2 text-3xl font-bold tabular-nums text-white"
    >
      <Clock aria-hidden="true" className="h-7 w-7 shrink-0" />
      <span>{text}</span>
    </div>
  );
}

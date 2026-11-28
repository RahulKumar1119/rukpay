import { Info } from "lucide-react";

import { COPY } from "../lib/copy";
import { NOTICE_BOX } from "./ui";

/**
 * Says in one sentence that the screen it sits on is a simulation. It is plain
 * text in an amber box, with an icon that only decorates. It has no close
 * control and no timer, so it stays for as long as the screen does
 * (Requirement 25.5).
 */
export function SimulationNotice() {
  return (
    <div role="note" className={NOTICE_BOX}>
      <p className="flex items-start gap-3 text-lg font-semibold">
        <Info aria-hidden="true" className="mt-0.5 h-6 w-6 shrink-0 text-amber-ink" />
        <span>{COPY.simulationNotice}</span>
      </p>
    </div>
  );
}

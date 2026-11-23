import { Loader2 } from "lucide-react";

import { COPY } from "../lib/copy";
import { PRESETS, type Preset } from "../lib/presets";
import { BUTTON_PRIMARY } from "./ui";

export interface PresetBarProps {
  /** True while the first request for a new payment is in flight. */
  busy: boolean;
  onSelect(preset: Preset): void;
}

/**
 * The three one-click scenarios. All three are disabled while a payment is
 * being checked, and a message that stays on screen says so (Requirement 2.10).
 */
export function PresetBar({ busy, onSelect }: PresetBarProps) {
  return (
    <section aria-labelledby="preset-bar-title" className="flex flex-col gap-3">
      <h2 id="preset-bar-title" className="text-xl font-semibold text-slate-surface">
        {COPY.presetBar.heading}
      </h2>
      <div role="group" aria-label={COPY.presetBar.groupLabel} className="flex flex-col gap-3 sm:flex-row sm:flex-wrap">
        {PRESETS.map((preset) => (
          <button
            key={preset.id}
            type="button"
            disabled={busy}
            onClick={() => onSelect(preset)}
            className={BUTTON_PRIMARY}
          >
            {preset.buttonLabel}
          </button>
        ))}
      </div>
      {busy && (
        <p role="status" className="flex items-center gap-3 text-lg font-semibold text-slate-surface">
          <Loader2 aria-hidden="true" className="h-6 w-6 shrink-0 animate-spin motion-reduce:animate-none" />
          <span>{COPY.presetBar.checking}</span>
        </p>
      )}
    </section>
  );
}

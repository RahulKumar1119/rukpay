import { useId, type ReactNode } from "react";

/**
 * A phone-shaped frame with a visible caption. The frame is a labelled region,
 * so a screen reader announces "Your phone" or "Family guardian's phone".
 * Children that need to cover the whole screen, such as the reality-check
 * modal, can use `absolute inset-0`, because the frame is positioned.
 */
export function PhoneFrame({ label, children }: { label: string; children: ReactNode }) {
  const captionId = useId();
  return (
    <div className="flex w-full max-w-sm flex-col gap-3">
      <h2 id={captionId} className="text-center text-xl font-semibold text-slate-surface">
        {label}
      </h2>
      <section
        aria-labelledby={captionId}
        className="relative flex min-h-[640px] flex-col overflow-hidden rounded-[2.5rem] border-[10px] border-slate-surface bg-white text-slate-surface shadow-xl"
      >
        <div aria-hidden="true" className="mx-auto mt-2 h-1.5 w-24 shrink-0 rounded-full bg-slate-surface" />
        <div className="flex flex-1 flex-col gap-4 overflow-y-auto p-5">{children}</div>
      </section>
    </div>
  );
}

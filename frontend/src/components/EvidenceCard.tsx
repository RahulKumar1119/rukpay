import { Ban, Copy, ExternalLink, Phone } from "lucide-react";
import { useId, useState } from "react";

import { COPY } from "../lib/copy";
import { BUTTON_PRIMARY, LINK_BUTTON } from "./ui";

type CopyState = "idle" | "copied" | "failed";

/**
 * What a person sees after a payment is blocked: that the transfer is cancelled
 * and no money moved, the summary they can share, and how to report the scam.
 *
 * The summary is shown as received, because the backend has already redacted
 * it. It is kept in the page only. The copy button writes it to the clipboard
 * when the person asks, and the result is shown as text that stays on screen.
 */
export function EvidenceCard({ summary }: { summary?: string }) {
  const titleId = useId();
  const [copyState, setCopyState] = useState<CopyState>("idle");

  async function copySummary(): Promise<void> {
    if (!summary) return;
    try {
      if (!navigator.clipboard) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(summary);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  }

  return (
    <section className="flex flex-col gap-4" aria-labelledby={titleId}>
      <div className="flex items-center gap-3">
        <Ban aria-hidden="true" className="h-8 w-8 shrink-0" />
        <h3 id={titleId} className="text-2xl font-bold">
          {COPY.evidence.title}
        </h3>
      </div>

      <p className="text-lg font-semibold">{COPY.evidence.cancelled}</p>

      <div className="flex flex-col gap-2">
        <h4 className="text-lg font-semibold">{COPY.evidence.summaryHeading}</h4>
        {summary ? (
          <pre className="select-text whitespace-pre-wrap break-words rounded-xl border-2 border-slate-surface bg-slate-50 p-3 font-sans text-base">
            {summary}
          </pre>
        ) : (
          <p className="text-base">{COPY.evidence.summaryMissing}</p>
        )}
        {summary && (
          <button type="button" onClick={() => void copySummary()} className={BUTTON_PRIMARY}>
            <Copy aria-hidden="true" className="h-5 w-5" />
            {COPY.evidence.copyButton}
          </button>
        )}
        <div aria-live="polite">
          {copyState === "copied" && <p className="text-lg font-semibold">{COPY.evidence.copied}</p>}
          {copyState === "failed" && <p className="text-lg font-semibold">{COPY.notices.COPY_FAILED}</p>}
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <h4 className="text-lg font-semibold">{COPY.evidence.reportHeading}</h4>
        <p className="text-base">{COPY.evidence.helplineLead}</p>
        <a href="tel:1930" aria-label={COPY.evidence.helplineLabel} className={LINK_BUTTON}>
          <Phone aria-hidden="true" className="h-5 w-5" />
          {COPY.evidence.helplineNumber}
        </a>
        <p className="text-base">{COPY.evidence.websiteLead}</p>
        <a
          href="https://cybercrime.gov.in"
          target="_blank"
          rel="noopener noreferrer"
          aria-label={COPY.evidence.websiteLabel}
          className={LINK_BUTTON}
        >
          <ExternalLink aria-hidden="true" className="h-5 w-5" />
          {COPY.evidence.websiteName}
        </a>
      </div>
    </section>
  );
}

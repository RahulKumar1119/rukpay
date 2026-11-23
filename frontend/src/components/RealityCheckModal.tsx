import { ArrowLeft, ShieldCheck } from "lucide-react";
import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";

import type { Answer, Answers } from "../lib/api";
import { COPY, questionProgress } from "../lib/copy";
import { UnreachableNotice } from "./UnreachableNotice";
import { BUTTON_AMBER } from "./ui";

export interface RealityCheckModalProps {
  open: boolean;
  /** Sends the three answers. The returned promise settles when the request is done. */
  onSubmit(answers: Answers): Promise<void>;
  /** The server could not be reached. The modal keeps the answers and offers a retry. */
  unreachable: boolean;
  onRetry(): void;
}

const FOCUSABLE = 'button:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';

/**
 * Three calm questions, one at a time, on the warm amber surface. It is a
 * modal dialog: focus moves in, stays in, and returns when it closes.
 *
 * Text is dark amber (#78350F) on #FEF3C7, 8.2:1, so it is readable at once.
 * There is no red anywhere in it. The answers are kept when the person goes
 * back, and when the server cannot be reached, so a retry sends the same three.
 */
export function RealityCheckModal({ open, onSubmit, unreachable, onRetry }: RealityCheckModalProps) {
  const titleId = useId();
  const questionId = useId();
  const [step, setStep] = useState(0);
  const [answers, setAnswers] = useState<(Answer | undefined)[]>([]);
  const [sending, setSending] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  const yesRef = useRef<HTMLButtonElement>(null);

  // A closed modal forgets its answers, so the next vault starts at question 1.
  useEffect(() => {
    if (!open) {
      setStep(0);
      setAnswers([]);
      setSending(false);
    }
  }, [open]);

  // Focus goes into the dialog when it opens and back to where it was when it closes.
  useEffect(() => {
    if (!open) return undefined;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    return () => {
      if (previous?.isConnected) previous.focus();
    };
  }, [open]);

  // Focus goes to the first answer button on each new question.
  useEffect(() => {
    if (!open) return;
    if (!unreachable && !sending) yesRef.current?.focus();
    else dialogRef.current?.focus();
  }, [open, step, unreachable, sending]);

  if (!open) return null;

  async function answer(value: Answer): Promise<void> {
    if (sending) return;
    const next = [...answers];
    next[step] = value;
    setAnswers(next);
    if (step < COPY.realityCheck.questions.length - 1) {
      setStep(step + 1);
      return;
    }
    const [coercion, secrecy, verification] = next;
    if (!coercion || !secrecy || !verification) return;

    setSending(true);
    try {
      await onSubmit({ coercion, secrecy, verification });
    } finally {
      setSending(false);
    }
  }

  // Tab and Shift+Tab loop over the controls inside the dialog.
  function keepFocusInside(event: KeyboardEvent<HTMLDivElement>): void {
    if (event.key !== "Tab") return;
    const root = dialogRef.current;
    if (!root) return;
    const controls = Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
    const first = controls[0];
    const last = controls[controls.length - 1];
    if (!first || !last) {
      event.preventDefault();
      root.focus();
      return;
    }
    const active = document.activeElement;
    if (event.shiftKey && (active === first || active === root)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  const earlier = answers[step];

  return (
    <div
      ref={dialogRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      tabIndex={-1}
      onKeyDown={keepFocusInside}
      className="absolute inset-0 z-10 flex flex-col gap-4 overflow-y-auto bg-amber-surface p-5 text-amber-text"
    >
      <div className="flex items-center gap-3">
        <ShieldCheck aria-hidden="true" className="h-8 w-8 shrink-0 text-amber-ink" />
        <h3 id={titleId} className="text-2xl font-bold">
          {COPY.realityCheck.title}
        </h3>
      </div>
      <p className="text-lg">{COPY.realityCheck.intro}</p>

      {unreachable ? (
        <UnreachableNotice onRetry={onRetry} />
      ) : sending ? (
        <p role="status" className="text-xl font-semibold">
          {COPY.realityCheck.sending}
        </p>
      ) : (
        <>
          <p className="text-lg font-semibold">{questionProgress(step + 1)}</p>
          <p id={questionId} className="text-xl font-semibold leading-snug">
            {COPY.realityCheck.questions[step]}
          </p>
          {earlier && (
            <p className="text-lg">
              {COPY.realityCheck.earlierAnswer} {earlier === "YES" ? COPY.realityCheck.yes : COPY.realityCheck.no}
            </p>
          )}
          <div className="flex gap-3">
            <button
              ref={yesRef}
              type="button"
              aria-describedby={questionId}
              onClick={() => void answer("YES")}
              className={`${BUTTON_AMBER} flex-1`}
            >
              {COPY.realityCheck.yes}
            </button>
            <button
              type="button"
              aria-describedby={questionId}
              onClick={() => void answer("NO")}
              className={`${BUTTON_AMBER} flex-1`}
            >
              {COPY.realityCheck.no}
            </button>
          </div>
          {step > 0 && (
            <button type="button" onClick={() => setStep(step - 1)} className={`${BUTTON_AMBER} self-start`}>
              <ArrowLeft aria-hidden="true" className="h-5 w-5" />
              {COPY.realityCheck.back}
            </button>
          )}
        </>
      )}
    </div>
  );
}

/**
 * Every string a person can read in the simulator, in one place.
 *
 * The wording follows `.kiro/steering/anti-panic-ux.md`: calm, plain English
 * at a 6th-grade level. The 15-minute lock is a protective pause, never an
 * accusation, and nothing here uses technical jargon or alarming phrases.
 *
 * Components import `COPY` and read from the group for their screen. Nothing
 * else in `src/` holds user-facing text, so `ALL_COPY` can be checked once for
 * banned terms (Requirement 13.1).
 *
 * Exact wording that the requirements fix, such as the button labels, the three
 * questions and the unreachable message, is kept character for character.
 */

export const COPY = {
  app: {
    title: "RukPay",
    tagline: "Pause before you pay.",
    userPhoneLabel: "Your phone",
    guardianPhoneLabel: "Family guardian's phone",
  },

  presetBar: {
    heading: "Try a payment",
    groupLabel: "Choose a payment to try",
    safeLabel: "Safe daily payment",
    mediumLabel: "Medium-risk unusual transfer",
    highLabel: "High-risk panic transfer",
    safePayee: "Local store",
    mediumPayee: "New contact",
    highPayee: "New account",
    checking: "Checking this payment…",
  },

  /** The payer's phone. `status` is keyed by the screen name from `lib/screens.ts`. */
  userPanel: {
    payingLabel: "Paying",
    amountLabel: "Amount",
    prompt: "Choose a payment above to see how RukPay keeps it safe.",
    status: {
      PROMPT: "Ready when you are",
      SENT: "Payment sent",
      CHECKING: "A few quick questions first",
      CONTINUE: "Checks complete",
      LOCKED: "Your money is safe in the vault",
      RELEASED: "Payment approved by your family",
      CANCELLED: "Payment cancelled",
      RECONFIRM: "One last check",
      DONE: "Payment confirmed",
    },
    sentMessage: "Your payment has been sent. Nothing looked unusual.",
    continueMessage: "Your checks look fine.",
    continueButton: "Continue payment",
    releasedMessage: "Your family guardian approved this payment. It is on its way.",
    reconfirmMessage:
      "The 15-minute safety pause is over. Take a moment to think. Do you still want to make this payment?",
    reconfirmYes: "Yes, this is my payment",
    reconfirmCancel: "Cancel and keep my money",
    doneMessage: "Thank you. Your payment is confirmed and on its way.",
    blockHint: "If something feels wrong, you can stop this payment at any time.",
    blockButton: "Block & Report (1930)",
  },

  /** The three questions are the exact wording from the steering document. */
  realityCheck: {
    title: "Quick safety check",
    intro: "Take a breath. There is no rush. We will ask one question at a time.",
    questions: [
      "Are you currently on a phone or video call with someone claiming to be from Police, CBI, Customs, RBI, or your Bank?",
      "Did the caller ask you NOT to tell your family or bank staff about this transfer?",
      "Have you independently verified this person or bill without using a phone number they just sent you?",
    ],
    yes: "Yes",
    no: "No",
    back: "Back",
    sending: "Checking your answers…",
  },

  vaultCard: {
    title: "Safety pause is on",
    pauseMessage: "We're holding this safely for 15 minutes so you have space to verify.",
    verifyHint: "Hang up, then call a family member or your bank on a number you already know.",
    timeLeft: "Time left",
    fastForward: "Demo: skip the wait",
  },

  guardianPanel: {
    idle: "Nothing needs your attention right now.",
    alertTitle: "Please check in with your family member",
    alertIntro: "A payment is on a 15-minute safety pause. Call them before you decide.",
    payingLabel: "Paying",
    amountLabel: "Amount",
    approve: "I Spoke With Them — Safe to Approve",
    veto: "Block & Report Scam (1930)",
    confirmQuestion: "Are you sure you spoke with them directly?",
    confirmYes: "Yes, approve",
    confirmBack: "Go back",
    approvedTitle: "Payment approved",
    approvedBody: "The payment is on its way.",
    refresh: "Refresh",
  },

  evidence: {
    title: "This payment was stopped",
    cancelled: "This transfer has been permanently cancelled. No money has left the account.",
    summaryHeading: "Summary you can share",
    summaryMissing: "The summary is not ready yet. You can still call 1930.",
    reportHeading: "Report it",
    helplineLead: "Call the National Cybercrime Helpline:",
    helplineNumber: "1930",
    helplineLabel: "Call 1930, the National Cybercrime Helpline",
    websiteLead: "Or report online:",
    websiteName: "cybercrime.gov.in",
    websiteLabel: "Open cybercrime.gov.in in a new tab",
    copyButton: "Copy summary",
    copied: "Summary copied.",
  },

  unreachable: {
    message: "We can't reach RukPay right now. Your money has not moved. Please try again.",
    retry: "Try again",
  },

  /** Keys match the `kind` of a notice in the simulator state. */
  notices: {
    BAD_REQUEST: "Something about that request did not look right. Please try again.",
    LINK_INVALID: "This approval link is no longer valid. Please refresh to see the latest.",
    ALREADY_ACTED: "Someone already acted on this payment.",
    COPY_FAILED: "Please select the text above and copy it by hand.",
  },

  common: {
    working: "One moment…",
  },
} as const;

/** How many questions the reality check asks. */
export const QUESTION_COUNT = COPY.realityCheck.questions.length;

/** The progress line for question number `current`, counting from 1: "Question 1 of 3". */
export function questionProgress(current: number): string {
  return `Question ${current} of ${QUESTION_COUNT}`;
}

function collect(value: unknown, out: string[]): void {
  if (typeof value === "string") {
    out.push(value);
  } else if (Array.isArray(value)) {
    value.forEach((item: unknown) => collect(item, out));
  } else if (typeof value === "object" && value !== null) {
    Object.values(value).forEach((item: unknown) => collect(item, out));
  }
}

function listAllCopy(): string[] {
  const out: string[] = [];
  collect(COPY, out);
  for (let n = 1; n <= QUESTION_COUNT; n++) out.push(questionProgress(n));
  return out;
}

/**
 * Every user-facing string, so one test can check all of it for banned terms.
 * It walks `COPY`, so a string added there is covered without a second edit.
 */
export const ALL_COPY: string[] = listAllCopy();

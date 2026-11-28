/**
 * Class names shared by the components, so every control follows the same
 * rules: at least 48 by 48 pixels, a clear label at 18 pixels or larger, no
 * double-tap zoom delay, and a hover and focus state.
 *
 * `bg-veto-red` appears only in `BUTTON_VETO`, which is used by the two
 * "Block & Report" buttons and nowhere else.
 */
const BUTTON =
  "inline-flex min-h-12 min-w-12 touch-manipulation items-center justify-center gap-2 rounded-xl px-5 py-3 text-lg font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-60";

/** The main action on a white surface. */
export const BUTTON_PRIMARY = `${BUTTON} bg-slate-surface text-slate-ink hover:bg-slate-700 active:bg-slate-900`;

/** A quieter action on a white surface. */
export const BUTTON_SECONDARY = `${BUTTON} border-2 border-slate-surface bg-white text-slate-surface hover:bg-slate-100 active:bg-slate-200`;

/** An answer button on the amber surface. */
export const BUTTON_AMBER = `${BUTTON} border-2 border-amber-ink bg-white text-amber-text hover:bg-amber-100 active:bg-amber-200`;

/** A button on the deep teal vault card. */
export const BUTTON_VAULT = `${BUTTON} bg-white text-vault-deep hover:bg-teal-50 active:bg-teal-100`;

/** The "Block & Report" button. Large bold text keeps white on red readable. */
export const BUTTON_VETO = `${BUTTON} bg-veto-red text-xl font-bold text-white hover:bg-red-700 active:bg-red-800`;

/** A link that looks and measures like a secondary button. */
export const LINK_BUTTON = `${BUTTON} border-2 border-slate-surface bg-white text-slate-surface underline hover:bg-slate-100`;

/** A calm inline block for a notice. */
export const NOTICE_BOX = "flex flex-col gap-3 rounded-2xl border-2 border-amber-ink bg-amber-surface p-4 text-amber-text";

// ---------------------------------------------------------------------------
// The saved-recipients and custom-payment screens
//
// These use only the colors the contrast check knows: white, slate-50,
// slate-100, slate-surface, slate-ink, amber-surface and amber-text, with
// slate-200, slate-700 and slate-900 for the hover and pressed backgrounds that
// the shipped buttons use too. Nothing here is red.
// ---------------------------------------------------------------------------

const CORE =
  "inline-flex min-h-12 min-w-12 touch-manipulation items-center justify-center gap-2 rounded-xl px-5 py-3 text-lg font-semibold transition-colors disabled:cursor-not-allowed";

/**
 * A disabled control keeps dark text on white and gets a dashed border. The
 * shipped buttons fade with `opacity-60`, which drops the text below 7:1, so
 * these do not (Requirement 26.3).
 */
const DISABLED_CLEAR =
  "disabled:border-2 disabled:border-dashed disabled:border-slate-surface disabled:bg-white disabled:text-slate-surface";

/** The main action of these screens: light text on the dark slate surface. */
export const BUTTON_ACTION = `${CORE} bg-slate-surface text-slate-ink hover:bg-slate-700 active:bg-slate-900 ${DISABLED_CLEAR}`;

/** A quieter action: dark text on white with a solid border. */
export const BUTTON_QUIET = `${CORE} border-2 border-slate-surface bg-white text-slate-surface hover:bg-slate-100 active:bg-slate-200 ${DISABLED_CLEAR}`;

/**
 * A text field. A placeholder keeps the same dark color as typed text, in
 * italics, so it also reaches 7:1. A read-only or disabled field stays dark on
 * a light background.
 */
export const FIELD =
  "block min-h-12 w-full rounded-xl border-2 border-slate-surface bg-white px-4 py-2 text-lg text-slate-surface placeholder:italic placeholder:text-slate-surface read-only:bg-slate-100 disabled:cursor-not-allowed disabled:border-dashed disabled:bg-white disabled:text-slate-surface";

/** A whole row that is the hit area of a switch or a radio, so the label is clickable too. */
export const CHOICE_ROW =
  "flex min-h-12 w-full cursor-pointer items-center gap-3 rounded-xl border-2 border-slate-surface bg-white px-4 py-2 text-lg text-slate-surface";

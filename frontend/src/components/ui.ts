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

/**
 * The three one-click scenarios. Each is a fixed payment request that the
 * backend scores as INSTANT_PASS, REALITY_CHECK or LOCKED_15M.
 *
 * A preset holds a masked identifier and a plain payee label only. It never
 * holds a raw account number, UPI id or phone number (Requirement 12.2).
 */
import { COPY } from "./copy";
import type { EvaluateRequest } from "./api";

export interface Preset {
  id: "safe" | "medium" | "high";
  /** The label on the preset button. */
  buttonLabel: string;
  /** A name only: "Local store", "New contact" or "New account". */
  payeeLabel: string;
  /** The identifier shown on the phone and sent to the backend, already masked. */
  payeeMasked: string;
  /** The time of day shown on the payer's phone. */
  timeLabel: string;
  request: EvaluateRequest;
}

const { presetBar } = COPY;

export const PRESETS: readonly [Preset, Preset, Preset] = [
  {
    id: "safe",
    buttonLabel: presetBar.safeLabel,
    payeeLabel: presetBar.safePayee,
    payeeMasked: "s***@okaxis",
    timeLabel: presetBar.safeTime,
    request: {
      amount_inr: 450,
      beneficiary_age_hours: 720,
      is_late_night: false,
      active_unknown_call: false,
      rapid_retry_count: 0,
      upi_masked: "s***@okaxis",
    },
  },
  {
    id: "medium",
    buttonLabel: presetBar.mediumLabel,
    payeeLabel: presetBar.mediumPayee,
    payeeMasked: "XXXX-XXXX-3307",
    timeLabel: presetBar.mediumTime,
    request: {
      amount_inr: 18000,
      beneficiary_age_hours: 6,
      is_late_night: false,
      active_unknown_call: false,
      rapid_retry_count: 0,
      beneficiary_masked: "XXXX-XXXX-3307",
    },
  },
  {
    id: "high",
    buttonLabel: presetBar.highLabel,
    payeeLabel: presetBar.highPayee,
    payeeMasked: "XXXX-XXXX-4821",
    timeLabel: presetBar.highTime,
    request: {
      amount_inr: 85000,
      beneficiary_age_hours: 1,
      is_late_night: true,
      active_unknown_call: true,
      rapid_retry_count: 0,
      beneficiary_masked: "XXXX-XXXX-4821",
    },
  },
];

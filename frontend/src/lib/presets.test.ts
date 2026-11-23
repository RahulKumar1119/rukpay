import { describe, expect, it } from "vitest";

import { PRESETS } from "./presets";

const MASKED_ACCOUNT = /^XXXX-XXXX-\d{4}$/;
const MASKED_PHONE = /^\+91-X{6}\d{4}$/;
const MASKED_UPI = /^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.-]+$/;

const isMasked = (s: string): boolean => [MASKED_ACCOUNT, MASKED_PHONE, MASKED_UPI].some((re) => re.test(s));

describe("PRESETS", () => {
  it("has exactly three presets, in the order safe, medium, high", () => {
    expect(PRESETS.map((p) => p.id)).toEqual(["safe", "medium", "high"]);
  });

  it("labels the buttons as the requirements say", () => {
    expect(PRESETS.map((p) => p.buttonLabel)).toEqual([
      "Safe daily payment",
      "Medium-risk unusual transfer",
      "High-risk panic transfer",
    ]);
  });

  it("names each payee with a label only", () => {
    expect(PRESETS.map((p) => p.payeeLabel)).toEqual(["Local store", "New contact", "New account"]);
  });

  it("shows the late time on the high-risk preset", () => {
    expect(PRESETS[2].timeLabel).toBe("11:45 PM");
    expect(PRESETS.every((p) => p.timeLabel.length > 0)).toBe(true);
  });

  it("sends the safe payment exactly", () => {
    expect(PRESETS[0].request).toEqual({
      amount_inr: 450,
      beneficiary_age_hours: 720,
      is_late_night: false,
      active_unknown_call: false,
      rapid_retry_count: 0,
      upi_masked: "s***@okaxis",
    });
  });

  it("sends the medium-risk transfer exactly", () => {
    expect(PRESETS[1].request).toEqual({
      amount_inr: 18000,
      beneficiary_age_hours: 6,
      is_late_night: false,
      active_unknown_call: false,
      rapid_retry_count: 0,
      beneficiary_masked: "XXXX-XXXX-3307",
    });
  });

  it("sends the high-risk transfer exactly", () => {
    expect(PRESETS[2].request).toEqual({
      amount_inr: 85000,
      beneficiary_age_hours: 1,
      is_late_night: true,
      active_unknown_call: true,
      rapid_retry_count: 0,
      beneficiary_masked: "XXXX-XXXX-4821",
    });
  });

  it("holds only masked identifiers", () => {
    for (const preset of PRESETS) {
      expect(isMasked(preset.payeeMasked), `${preset.id} payeeMasked`).toBe(true);

      const { upi_masked, beneficiary_masked, payer_account_masked, phone_masked } = preset.request;
      const sent = [upi_masked, beneficiary_masked, payer_account_masked, phone_masked].filter(
        (value): value is string => value !== undefined,
      );
      expect(sent.length, `${preset.id} sends one identifier`).toBeGreaterThan(0);
      sent.forEach((value) => expect(isMasked(value), `${preset.id} ${value}`).toBe(true));
    }
  });

  it("sends the same identifier that the phone shows", () => {
    for (const preset of PRESETS) {
      const { upi_masked, beneficiary_masked } = preset.request;
      expect(upi_masked ?? beneficiary_masked).toBe(preset.payeeMasked);
    }
  });

  it("puts a UPI id in upi_masked and an account in beneficiary_masked", () => {
    expect(PRESETS[0].request.upi_masked).toBeDefined();
    expect(PRESETS[0].request.beneficiary_masked).toBeUndefined();
    for (const preset of [PRESETS[1], PRESETS[2]]) {
      expect(preset.request.beneficiary_masked).toBeDefined();
      expect(preset.request.upi_masked).toBeUndefined();
    }
  });

  it("holds no OTP, PIN or password field", () => {
    for (const preset of PRESETS) {
      expect(Object.keys(preset.request).filter((key) => /^(otp|pin|password|passcode|cvv)$/i.test(key))).toEqual([]);
    }
  });
});

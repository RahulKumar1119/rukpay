import { describe, expect, it } from "vitest";

import { ALL_COPY, COPY, NEW_COPY_GROUPS } from "./copy";

/**
 * The strings that the two new screens share: the simulation notice and the
 * names of the phone's three screens. The calm-copy rules for them are checked
 * by `copy.new.pbt.test.ts` through NEW_COPY_GROUPS. This file pins the texts
 * and the lists that make that check cover them.
 */
describe("the copy the new screens share", () => {
  it("has the simulation notice as the design's one sentence", () => {
    expect(COPY.simulationNotice).toBe("This is a simulation of how RukPay treats payments. No real payment is made, held or cancelled.");
  });

  it("says in the notice that the screen is a simulation and that no real payment moves", () => {
    expect(COPY.simulationNotice).toMatch(/simulation/i);
    expect(COPY.simulationNotice).toMatch(/no real payment/i);
  });

  it("names the phone's three screens in the order the navigation shows them", () => {
    expect(COPY.phoneNav).toEqual({
      groupLabel: "Phone screens",
      pay: "Pay",
      custom: "Custom payment",
      saved: "Saved recipients",
    });
    expect(Object.keys(COPY.phoneNav)).toEqual(["groupLabel", "pay", "custom", "saved"]);
  });

  it("gives the three screens three different names", () => {
    const names = [COPY.phoneNav.pay, COPY.phoneNav.custom, COPY.phoneNav.saved];
    expect(new Set(names).size).toBe(3);
    for (const name of names) expect(name.trim().length).toBeGreaterThan(0);
  });

  it("is in NEW_COPY_GROUPS, next to the group that was there before", () => {
    expect(NEW_COPY_GROUPS).toEqual(expect.arrayContaining(["explanation", "simulationNotice", "phoneNav"]));
    for (const group of NEW_COPY_GROUPS) expect(Object.keys(COPY)).toContain(group);
  });

  it("is in ALL_COPY, string by string", () => {
    for (const text of [COPY.simulationNotice, COPY.phoneNav.groupLabel, COPY.phoneNav.pay, COPY.phoneNav.custom, COPY.phoneNav.saved]) {
      expect(ALL_COPY).toContain(text);
    }
  });
});

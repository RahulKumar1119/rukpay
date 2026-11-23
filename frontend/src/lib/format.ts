/**
 * Number formatting for the screens, through `Intl` so the grouping follows the
 * Indian convention: 85000 shows as ₹85,000.
 */
const rupees = new Intl.NumberFormat("en-IN", {
  style: "currency",
  currency: "INR",
  minimumFractionDigits: 0,
  maximumFractionDigits: 0,
});

/** An amount in whole rupees with the ₹ sign. */
export function formatRupees(amount: number): string {
  return rupees.format(amount);
}

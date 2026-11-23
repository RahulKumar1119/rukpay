import type { Config } from "tailwindcss";

// The RukPay palette. It follows the anti-panic rules: warm amber and calm slate
// surfaces for the reality check, deep teal for "your money is safe in the
// vault", and red only for the "Block & Report" buttons. Pure red (#FF0000) is
// never used.
//
// Contrast (WCAG): alert text and the countdown must reach 7:1.
//   amber-ink  #92400E on amber-surface  6.4:1  too low for text; used for icons and borders
//   amber-text #78350F on amber-surface  8.2:1  the text color on the amber surface
//   slate-ink  #F8FAFC on slate-surface 14.0:1
//   white on vault-teal #0F766E          5.5:1  too low for text; vault-teal is the frame
//   white on vault-deep #134E4A          9.5:1  the surface that holds the text on the vault card
//   white on veto-red   #DC2626          4.8:1  allowed only for large bold button labels
const config: Config = {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        "amber-surface": "#FEF3C7",
        "amber-ink": "#92400E",
        "amber-text": "#78350F",
        "slate-surface": "#1E293B",
        "slate-ink": "#F8FAFC",
        "vault-teal": "#0F766E",
        "vault-deep": "#134E4A",
        "veto-red": "#DC2626",
      },
    },
  },
  plugins: [],
};

export default config;

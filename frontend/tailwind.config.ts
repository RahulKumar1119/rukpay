import type { Config } from "tailwindcss";

// The RukPay palette. It follows the anti-panic rules: warm amber and calm slate
// surfaces for the reality check, deep teal for "your money is safe in the
// vault", and red only for the "Block & Report" buttons. Pure red (#FF0000) is
// never used.
const config: Config = {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        "amber-surface": "#FEF3C7",
        "amber-ink": "#92400E",
        "slate-surface": "#1E293B",
        "slate-ink": "#F8FAFC",
        "vault-teal": "#0F766E",
        "veto-red": "#DC2626",
      },
    },
  },
  plugins: [],
};

export default config;

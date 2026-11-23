import { GuardianPhonePanel } from "./components/GuardianPhonePanel";
import { PresetBar } from "./components/PresetBar";
import { UserPhonePanel } from "./components/UserPhonePanel";
import { COPY } from "./lib/copy";
import { useSimulator } from "./lib/useSimulator";

/**
 * Both phones side by side. At 1024 pixels and wider the payer is on the left
 * and the family guardian on the right, so one screen shows the whole flow. Below
 * that they stack. There is no login, no sign-up and nothing to set up: press a
 * preset and watch.
 */
export default function App() {
  const sim = useSimulator();

  return (
    <main className="mx-auto flex min-h-screen w-full max-w-6xl flex-col gap-6 px-4 py-6 lg:px-8">
      <header className="flex flex-col gap-1">
        <h1 className="text-3xl font-bold text-slate-surface">{COPY.app.title}</h1>
        <p className="text-lg text-slate-surface">{COPY.app.tagline}</p>
      </header>

      <PresetBar busy={sim.state.busy} onSelect={(preset) => void sim.startPreset(preset)} />

      <div className="flex flex-col items-center gap-6 lg:flex-row lg:items-start lg:justify-center">
        <UserPhonePanel sim={sim} />
        <GuardianPhonePanel sim={sim} />
      </div>
    </main>
  );
}

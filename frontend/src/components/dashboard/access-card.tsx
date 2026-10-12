const CAN = [
  ["jobs:read", "Watch the pipeline, metrics, and analytics live"],
  ["jobs:read", "Inspect any stage and open job details and attempts"],
  ["jobs:read", "Browse and filter the full job history"],
] as const;

const NEEDS = [
  ["jobs:write", "Submit jobs and batches"],
  ["jobs:cancel", "Cancel queued or running jobs"],
] as const;

function Mark({ ok }: { ok: boolean }) {
  return (
    <span
      className={`mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full text-[10px] font-bold ${ok ? "bg-emerald-950 text-emerald-300" : "bg-zinc-800 text-zinc-500"}`}
      aria-hidden="true"
    >
      {ok ? "✓" : "–"}
    </span>
  );
}

/** Shown instead of the submit form when the API key lacks jobs:write: what this key can do, and how to get more. */
export function AccessCard({ scopes }: { scopes: string[] }) {
  return (
    <div className="space-y-4 text-sm">
      <p className="text-zinc-300">
        You are signed in with a <span className="font-semibold text-zinc-50">read-only</span> API key. You can watch
        everything, but you can&apos;t change anything.
      </p>
      <div className="flex flex-wrap gap-1.5" aria-label="Scopes on this key">
        {scopes.map((s) => (
          <code key={s} className="rounded-full bg-zinc-800 px-2 py-0.5 text-xs text-zinc-300">
            {s}
          </code>
        ))}
      </div>
      <ul className="space-y-2">
        {CAN.map(([scope, text]) => (
          <li key={text} className="flex gap-2">
            <Mark ok={scopes.includes(scope)} />
            <span className="text-zinc-300">{text}</span>
          </li>
        ))}
        {NEEDS.map(([scope, text]) => (
          <li key={text} className="flex gap-2">
            <Mark ok={scopes.includes(scope)} />
            <span className="text-zinc-500">
              {text} <span className="text-xs">(needs <code>{scope}</code>)</span>
            </span>
          </li>
        ))}
      </ul>
      <div className="rounded-lg border border-zinc-800 bg-zinc-950 p-3">
        <p className="font-medium text-zinc-200">Need to submit jobs?</p>
        <p className="mt-1 text-zinc-400">
          Ask an operator for a key with write access, then sign out and sign in with it. The operator runs:
        </p>
        <pre className="mt-2 rounded-md bg-zinc-900 p-2 font-mono text-xs break-all whitespace-pre-wrap text-zinc-300">
          taskpilot-admin key create -tenant &lt;tenant-id&gt; -name &lt;name&gt; -scopes jobs:read,jobs:write,jobs:cancel
        </pre>
      </div>
    </div>
  );
}

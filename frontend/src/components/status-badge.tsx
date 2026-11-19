const NEUTRAL = "bg-zinc-100 text-zinc-700 ring-zinc-300 dark:bg-zinc-800 dark:text-zinc-300 dark:ring-zinc-600";
const FAILED = "bg-rose-50 text-rose-700 ring-rose-300 dark:bg-rose-950 dark:text-rose-300 dark:ring-rose-800";

// Job states plus attempt-only states (docs/05-JOB-LIFECYCLE.md §2, §4).
const STYLES: Record<string, string> = {
  queued: NEUTRAL,
  running: "bg-sky-50 text-sky-700 ring-sky-300 dark:bg-sky-950 dark:text-sky-300 dark:ring-sky-800",
  succeeded: "bg-emerald-50 text-emerald-700 ring-emerald-300 dark:bg-emerald-950 dark:text-emerald-300 dark:ring-emerald-800",
  failed: FAILED,
  timed_out: FAILED,
  lease_expired: FAILED,
  cancelled: "bg-amber-50 text-amber-800 ring-amber-300 dark:bg-amber-950 dark:text-amber-300 dark:ring-amber-800",
};

/** Status shown as text plus colour, never colour alone. */
export function StatusBadge({ status, cancelRequested = false }: { status: string; cancelRequested?: boolean }) {
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${STYLES[status] ?? NEUTRAL}`}>
      {status.replace("_", " ")}
      {cancelRequested && status === "running" ? " · cancel requested" : ""}
    </span>
  );
}

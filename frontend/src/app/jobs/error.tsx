"use client";

export default function JobsError({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return (
    <div role="alert" className="rounded-lg border border-rose-200 bg-rose-50 p-6 dark:border-rose-900 dark:bg-rose-950/40">
      <h1 className="font-semibold text-rose-800 dark:text-rose-300">Something went wrong</h1>
      <p className="mt-1 text-sm text-rose-700 dark:text-rose-400">
        The TaskPilot API could not be reached or returned an error.
        {error.digest && <> Reference: <code>{error.digest}</code></>}
      </p>
      <button onClick={retry} className="mt-4 rounded-md border border-rose-300 px-3 py-1.5 text-sm hover:bg-rose-100 dark:border-rose-800 dark:hover:bg-rose-900">
        Try again
      </button>
    </div>
  );
}

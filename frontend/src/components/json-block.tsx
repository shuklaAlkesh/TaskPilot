/** Untrusted JSON rendered as escaped text, never as HTML (docs/16-FRONTEND.md §6). */
export function JsonBlock({ value }: { value: unknown }) {
  return (
    <pre className="max-h-96 overflow-auto rounded-lg bg-zinc-900 p-4 font-mono text-xs leading-relaxed text-zinc-100">
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}

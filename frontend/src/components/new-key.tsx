"use client";

import { useState } from "react";

/** A freshly created API key: shown once, with a copy button. */
export function NewKey({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="space-y-2 rounded-md border border-amber-900 bg-amber-950/30 p-3 text-sm">
      <p className="font-medium text-amber-200">Copy this API key now. It won&apos;t be shown again.</p>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 rounded bg-zinc-950 px-2 py-1.5 font-mono text-xs break-all text-zinc-100">{value}</code>
        <button
          type="button"
          onClick={() => navigator.clipboard.writeText(value).then(() => setCopied(true), () => setCopied(false))}
          className="shrink-0 rounded-md border border-zinc-700 px-2.5 py-1 text-xs hover:bg-zinc-800"
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <p className="text-xs text-zinc-400">Your application sends it as <code>Authorization: Bearer &lt;key&gt;</code>.</p>
    </div>
  );
}

"use client";

import { useActionState } from "react";

import { createKey, type KeyState } from "@/app/actions";
import { NewKey } from "@/components/new-key";

export function CreateKeyForm() {
  const [state, action, pending] = useActionState(createKey, {} as KeyState);
  return (
    <div className="space-y-3">
      <form action={action} className="flex flex-wrap items-end gap-3">
        <div className="min-w-48 flex-1">
          <label htmlFor="key-name" className="block text-sm font-medium text-zinc-300">
            New key name
          </label>
          <input
            id="key-name"
            name="name"
            required
            maxLength={100}
            placeholder="production"
            className="mt-1 w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-indigo-500"
          />
        </div>
        <button disabled={pending} className="rounded-md border border-zinc-700 px-4 py-2 text-sm font-medium hover:bg-zinc-800 disabled:opacity-60">
          {pending ? "Creating…" : "Create API key"}
        </button>
      </form>
      {state.error && (
        <p role="alert" className="text-sm text-rose-400">
          {state.error}
        </p>
      )}
      {state.apiKey && <NewKey value={state.apiKey} />}
    </div>
  );
}

"use client";

import Link from "next/link";
import { useActionState } from "react";

import { createApp, type AppState } from "@/app/actions";
import { NewKey } from "@/components/new-key";

export function CreateAppForm() {
  const [state, action, pending] = useActionState(createApp, {} as AppState);
  if (state.apiKey) {
    return (
      <div className="space-y-3">
        <p role="status" className="text-sm text-emerald-300">
          Application <span className="font-semibold">{state.appName}</span> created.
        </p>
        <NewKey value={state.apiKey} />
        <div className="flex flex-wrap gap-3 text-sm">
          <Link href="/connect" className="rounded-md bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-500">
            Connect your app →
          </Link>
          <Link href="/" className="rounded-md border border-zinc-700 px-3 py-1.5 hover:bg-zinc-800">
            Open dashboard
          </Link>
        </div>
      </div>
    );
  }
  return (
    <form action={action} className="flex flex-wrap items-end gap-3">
      <div className="min-w-56 flex-1">
        <label htmlFor="name" className="block text-sm font-medium text-zinc-300">
          Application name
        </label>
        <input
          id="name"
          name="name"
          required
          maxLength={100}
          placeholder="billing-service"
          aria-describedby={state.error ? "app-error" : undefined}
          className="mt-1 w-full rounded-md border border-zinc-800 bg-zinc-950 px-3 py-2 text-sm text-zinc-100 placeholder:text-zinc-600 focus:border-indigo-500"
        />
      </div>
      <button disabled={pending} className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-60">
        {pending ? "Creating…" : "Create application"}
      </button>
      {state.error && (
        <p id="app-error" role="alert" className="w-full text-sm text-rose-400">
          {state.error}
        </p>
      )}
    </form>
  );
}

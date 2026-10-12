"use client";

import { useActionState } from "react";

import { signIn, type FormState } from "@/app/actions";

export function LoginForm() {
  const [state, action, pending] = useActionState(signIn, {} as FormState);
  return (
    <form action={action} className="mt-6 space-y-4">
      <div>
        <label htmlFor="api_key" className="block text-sm font-medium">
          API key
        </label>
        <input
          id="api_key"
          name="api_key"
          type="password"
          required
          autoComplete="off"
          spellCheck={false}
          placeholder="tp_xxxxxxxxxxxx_…"
          aria-describedby={state.error ? "login-error" : undefined}
          className="mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 font-mono text-sm dark:border-zinc-700 dark:bg-zinc-950"
        />
      </div>
      {state.error && (
        <p id="login-error" role="alert" className="text-sm text-rose-600 dark:text-rose-400">
          {state.error}
        </p>
      )}
      <button
        disabled={pending}
        className="w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-60"
      >
        {pending ? "Checking…" : "Sign in"}
      </button>
    </form>
  );
}

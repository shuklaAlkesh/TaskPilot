"use client";

import { useActionState, useState } from "react";

import { requestCode, verifyCode, type SignInState } from "@/app/actions";

const input =
  "mt-1 w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm dark:border-zinc-700 dark:bg-zinc-950";
const button =
  "w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:opacity-60";

function ErrorText({ id, error }: { id: string; error?: string | undefined }) {
  if (!error) return null;
  return (
    <p id={id} role="alert" className="text-sm text-rose-600 dark:text-rose-400">
      {error}
    </p>
  );
}

/** Email → one-time code. The code is emailed (or printed in the API log during local development). */
export function LoginForm() {
  const [sent, sendAction, sending] = useActionState(requestCode, {} as SignInState);
  const [verified, verifyAction, verifying] = useActionState(verifyCode, {} as SignInState);
  const [changeEmail, setChangeEmail] = useState(false);
  const email = sent.email ?? "";
  const onCodeStep = sent.codeSent && !changeEmail;

  if (!onCodeStep) {
    return (
      <form action={(form) => { setChangeEmail(false); sendAction(form); }} className="mt-6 space-y-4">
        <div>
          <label htmlFor="email" className="block text-sm font-medium">
            Email
          </label>
          <input
            id="email"
            name="email"
            type="email"
            required
            autoComplete="email"
            defaultValue={email}
            placeholder="you@company.com"
            aria-describedby={sent.error ? "email-error" : undefined}
            className={input}
          />
        </div>
        <ErrorText id="email-error" error={sent.error} />
        <button disabled={sending} className={button}>
          {sending ? "Sending…" : "Email me a sign-in code"}
        </button>
      </form>
    );
  }

  return (
    <form action={verifyAction} className="mt-6 space-y-4">
      <p role="status" className="rounded-md border border-emerald-900 bg-emerald-950/40 p-3 text-sm text-emerald-300">
        If <span className="font-medium">{email}</span> is invited, a 6-digit code is on its way. It expires in 10 minutes.
      </p>
      <input type="hidden" name="email" value={email} />
      <div>
        <label htmlFor="code" className="block text-sm font-medium">
          Sign-in code
        </label>
        <input
          id="code"
          name="code"
          required
          autoFocus
          inputMode="numeric"
          autoComplete="one-time-code"
          pattern="[0-9 ]{6,7}"
          maxLength={7}
          placeholder="123456"
          aria-describedby={verified.error ? "code-error" : undefined}
          className={`${input} font-mono tracking-widest`}
        />
      </div>
      <ErrorText id="code-error" error={verified.error} />
      <button disabled={verifying} className={button}>
        {verifying ? "Checking…" : "Sign in"}
      </button>
      <button type="button" onClick={() => setChangeEmail(true)} className="w-full text-sm text-zinc-400 hover:text-white">
        Use a different email or send a new code
      </button>
    </form>
  );
}

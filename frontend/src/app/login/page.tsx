import type { Metadata } from "next";
import { redirect } from "next/navigation";

import { getSession } from "@/lib/session";
import { LoginForm } from "./login-form";

export const metadata: Metadata = { title: "Sign in" };

export default async function LoginPage() {
  if (await getSession()) redirect("/");
  return (
    <div className="mx-auto mt-12 max-w-md rounded-xl border border-zinc-200 bg-white p-8 shadow-sm dark:border-zinc-800 dark:bg-zinc-900">
      <h1 className="text-xl font-semibold">Sign in to TaskPilot</h1>
      <p className="mt-2 text-sm text-zinc-600 dark:text-zinc-400">
        Paste a tenant API key. It is checked with the API and kept in an encrypted, HttpOnly cookie. Your
        browser&apos;s JavaScript never sees it again.
      </p>
      <LoginForm />
    </div>
  );
}

import type { Metadata } from "next";
import Link from "next/link";

import { getSession } from "@/lib/session";
import "./globals.css";

export const metadata: Metadata = {
  title: { default: "TaskPilot", template: "%s · TaskPilot" },
  description: "Submit, track, and cancel background jobs.",
};

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const session = await getSession();
  return (
    <html lang="en" className="dark">
      <body className="min-h-dvh">
        <header className="border-b border-zinc-800 bg-zinc-950/80 backdrop-blur">
          <nav className="mx-auto flex max-w-7xl flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3" aria-label="Main">
            <Link href="/" className="flex items-center gap-2 font-semibold tracking-tight">
              <svg viewBox="0 0 24 24" aria-hidden="true" className="size-5 fill-indigo-600">
                <path d="M3 12 21 3l-5 18-4-7-9-2Z" />
              </svg>
              TaskPilot
            </Link>
            {session && (
              <>
                <Link href="/" className="text-sm text-zinc-400 hover:text-white">
                  Dashboard
                </Link>
                <Link href="/jobs" className="text-sm text-zinc-400 hover:text-white">
                  Jobs
                </Link>
                <div className="ml-auto flex items-center gap-3 text-sm">
                  <span className="text-zinc-500" title="Tenant">
                    {session.tenantName}
                  </span>
                  <form action="/logout" method="post">
                    <button className="rounded-md border border-zinc-300 px-2.5 py-1 hover:bg-zinc-100 dark:border-zinc-700 dark:hover:bg-zinc-800">
                      Sign out
                    </button>
                  </form>
                </div>
              </>
            )}
          </nav>
        </header>
        <main className="mx-auto max-w-7xl px-4 py-6">{children}</main>
      </body>
    </html>
  );
}

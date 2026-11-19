import type { Metadata } from "next";

import { selectApp } from "@/app/actions";
import { account, unwrap } from "@/lib/api/client";
import { CreateAppForm } from "./create-app-form";

export const metadata: Metadata = { title: "Applications" };

const dateFmt = new Intl.DateTimeFormat("en", { dateStyle: "medium" });

/** Choose the application to look at, or register a new one. Each application's jobs are separate. */
export default async function AppsPage() {
  const { client, session } = await account();
  const { items: apps } = unwrap(await client.GET("/v1/apps"));
  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-zinc-50">Applications</h1>
        <p className="text-sm text-zinc-400">
          An application is anything that sends tasks to TaskPilot: a Django, FastAPI, Flask, Go or Node.js service.
          Each one has its own API keys, task types, and jobs.
        </p>
      </div>

      <section className="rounded-xl border border-zinc-800 bg-zinc-900 p-5" aria-label="Your applications">
        <h2 className="mb-3 text-sm font-semibold tracking-wide text-zinc-300 uppercase">Your applications</h2>
        {apps.length === 0 ? (
          <p className="text-sm text-zinc-500">You don&apos;t have an application yet. Create one below to get an API key.</p>
        ) : (
          <ul className="divide-y divide-zinc-800">
            {apps.map((app) => (
              <li key={app.id} className="flex flex-wrap items-center gap-3 py-2.5">
                <span className="font-medium text-zinc-100">{app.name}</span>
                <span className="text-xs text-zinc-500">created {dateFmt.format(new Date(app.created_at))}</span>
                {app.id === session.appId && <span className="rounded-full bg-indigo-950 px-2 py-0.5 text-xs text-indigo-300">current</span>}
                <form action={selectApp.bind(null, app.id)} className="ml-auto">
                  <button className="rounded-md border border-zinc-700 px-3 py-1 text-sm hover:bg-zinc-800">Open dashboard</button>
                </form>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="rounded-xl border border-zinc-800 bg-zinc-900 p-5" aria-label="Register an application">
        <h2 className="mb-3 text-sm font-semibold tracking-wide text-zinc-300 uppercase">Register an application</h2>
        <CreateAppForm />
      </section>
    </div>
  );
}

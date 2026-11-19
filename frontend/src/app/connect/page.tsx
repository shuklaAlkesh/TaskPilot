import type { Metadata } from "next";

import { api, unwrap } from "@/lib/api/client";
import { CreateKeyForm } from "./create-key-form";

export const metadata: Metadata = { title: "Connect" };

const dateFmt = new Intl.DateTimeFormat("en", { dateStyle: "medium" });

function Step({ n, title, children }: { n: number; title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-xl border border-zinc-800 bg-zinc-900 p-5" aria-label={title}>
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold text-zinc-200">
        <span className="flex size-6 items-center justify-center rounded-full bg-indigo-950 text-xs text-indigo-300">{n}</span>
        {title}
      </h2>
      {children}
    </section>
  );
}

/** A JSON value as a Python literal (True/False/None), for the SDK snippet. */
function py(v: unknown): string {
  if (v === null || v === undefined) return "None";
  if (typeof v === "boolean") return v ? "True" : "False";
  if (Array.isArray(v)) return `[${v.map(py).join(", ")}]`;
  if (typeof v === "object") return `{${Object.entries(v).map(([k, x]) => `${JSON.stringify(k)}: ${py(x)}`).join(", ")}}`;
  return JSON.stringify(v);
}

function Code({ children }: { children: string }) {
  return <pre className="overflow-x-auto rounded-md bg-zinc-950 p-3 font-mono text-xs leading-relaxed text-zinc-200">{children}</pre>;
}

/** How to connect the selected application: an API key, a worker for its tasks, and submitting jobs. */
export default async function ConnectPage() {
  const { client, session } = await api();
  const [keys, types] = await Promise.all([
    client.GET("/v1/apps/{app_id}/keys", { params: { path: { app_id: session.appId! } } }).then(unwrap),
    client.GET("/v1/task-types").then(unwrap),
  ]);
  const apiUrl = process.env.TASKPILOT_API_BASE_URL ?? "http://localhost:8080";
  const own = types.items.find((t) => !t.shared) ?? types.items[0];
  const taskType = own?.task_type ?? "emails.send_welcome";
  const example = own?.example ?? { user_id: 42 };
  const payload = JSON.stringify(example);

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight text-zinc-50">Connect {session.tenantName}</h1>
        <p className="text-sm text-zinc-400">
          Your application talks to TaskPilot over a REST API with an API key. Any language works; Python has an SDK.
        </p>
      </div>

      <Step n={1} title="Get an API key">
        {keys.items.length > 0 && (
          <ul className="mb-4 divide-y divide-zinc-800 text-sm">
            {keys.items.map((k) => (
              <li key={k.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
                <span className="font-medium text-zinc-100">{k.name}</span>
                <code className="text-xs text-zinc-500">tp_{k.prefix}_…</code>
                <span className="ml-auto text-xs text-zinc-500">
                  created {dateFmt.format(new Date(k.created_at))}
                  {k.expires_at && ` · expires ${dateFmt.format(new Date(k.expires_at))}`}
                </span>
              </li>
            ))}
          </ul>
        )}
        <CreateKeyForm />
      </Step>

      <Step n={2} title="Run a worker for your tasks">
        <p className="mb-3 text-sm text-zinc-400">
          Define tasks with the Python SDK, then start a worker. Passing your API key makes its task types belong to this
          application, so no other application can see or run them. Your TaskPilot operator gives you the worker URL and token.
        </p>
        <Code>{`# yourapp/tasks.py
from taskpilot_worker.registry import Payload, TaskRegistry

registry = TaskRegistry()

class WelcomeEmail(Payload):
    user_id: int

@registry.task("emails.send_welcome")
def send_welcome(p: WelcomeEmail, ctx) -> dict:
    ...  # your code; return a JSON-serialisable result
    return {"sent": True}`}</Code>
        <div className="mt-3">
          <Code>{`TASKPILOT_API_KEY=<your API key> \\
TASKPILOT_API_INTERNAL_URL=<worker URL> TASKPILOT_WORKER_TOKEN=<worker token> \\
TASKPILOT_WORKER_APP=yourapp.tasks:registry \\
python -m taskpilot_worker`}</Code>
        </div>
      </Step>

      <Step n={3} title="Submit tasks from your application">
        <p className="mb-3 text-sm text-zinc-400">From any language, over HTTP:</p>
        <Code>{`curl -X POST ${apiUrl}/v1/jobs \\
  -H "Authorization: Bearer $TASKPILOT_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"task_type":"${taskType}","task_version":1,"payload":${payload}}'`}</Code>
        <p className="mt-4 mb-3 text-sm text-zinc-400">Or with the Python SDK:</p>
        <Code>{`import os
from taskpilot_worker.producer import TaskPilot

with TaskPilot("${apiUrl}", api_key=os.environ["TASKPILOT_API_KEY"]) as tp:
    job = tp.submit("${taskType}", ${py(example)})
    print(tp.wait(job["id"])["status"])`}</Code>
        <p className="mt-3 text-sm text-zinc-400">
          Then watch it on the dashboard. The full API is in <code>backend/api/openapi.yaml</code>.
        </p>
      </Step>
    </div>
  );
}

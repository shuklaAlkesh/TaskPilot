import type { Metadata } from "next";

import { Dashboard } from "@/components/dashboard/dashboard";
import type { TaskOption } from "@/components/dashboard/submit-panel";
import { api, unwrap } from "@/lib/api/client";
import { fieldsFromSchema } from "@/lib/payload";

export const metadata: Metadata = { title: "Dashboard" };

const WINDOWS = ["15m", "1h", "6h", "24h"] as const;

export default async function DashboardPage({ searchParams }: { searchParams: Promise<{ window?: string }> }) {
  const requested = (await searchParams).window;
  const window = WINDOWS.find((w) => w === requested) ?? "1h";
  const { client, session } = await api();
  const [stats, recent, types] = await Promise.all([
    client.GET("/v1/stats", { params: { query: { window } } }).then(unwrap),
    client.GET("/v1/jobs", { params: { query: { limit: 8 } } }).then(unwrap),
    client.GET("/v1/task-types").then(unwrap),
  ]);
  const options: TaskOption[] = types.items.map((t) => ({
    value: `${t.task_type}@${t.task_version}`,
    label: `${t.title ?? t.task_type} (${t.task_type} v${t.task_version})`,
    fields: fieldsFromSchema(t.payload_schema),
    example: t.example ?? null,
    queue: t.queue,
    workersOnline: t.workers_online,
    defaultTimeoutSeconds: t.default_timeout_seconds,
    maxTimeoutSeconds: t.max_timeout_seconds,
    defaultMaxAttempts: t.default_max_attempts,
  }));
  return (
    <Dashboard
      initial={{ stats, recent: recent.items }}
      initialWindow={window}
      options={options}
      taskTypes={types.items}
      scopes={session.scopes}
    />
  );
}

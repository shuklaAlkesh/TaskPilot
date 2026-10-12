"use server";

import { redirect } from "next/navigation";

import { api, clientFor, type Problem } from "@/lib/api/client";
import { BATCH_SIZES, buildBatch } from "@/lib/batch";
import { fieldsFromSchema, payloadFromForm } from "@/lib/payload";
import { startSession } from "@/lib/session";

const API_KEY_PATTERN = /^tp_[a-z0-9]{12}_[A-Za-z0-9_-]{43}$/;
const BATCH_CONCURRENCY = 16;

export type FormState = {
  error?: string;
  fields?: { field: string; message: string }[];
  requestId?: string;
  jobId?: string;
  replayed?: boolean;
};

function problemState(problem: Problem | undefined, fallback: string): FormState {
  if (!problem) return { error: fallback };
  const state: FormState = { error: problem.title, requestId: problem.request_id };
  if (problem.errors?.length) state.fields = problem.errors;
  return state;
}

/** Validates the key with GET /v1/me, then stores it in the encrypted session cookie. */
export async function signIn(_prev: FormState, form: FormData): Promise<FormState> {
  const apiKey = String(form.get("api_key") ?? "").trim();
  if (!API_KEY_PATTERN.test(apiKey)) {
    return { error: "That doesn't look like a TaskPilot API key (tp_<prefix>_<secret>)." };
  }
  let me;
  try {
    const res = await clientFor(apiKey).GET("/v1/me");
    if (!res.data) {
      return { error: res.response.status === 401 ? "Invalid, revoked, or expired API key." : "Sign-in failed." };
    }
    me = res.data;
  } catch {
    return { error: "Can't reach the TaskPilot API. Is it running?" };
  }
  await startSession(apiKey, me.tenant_name, me.scopes);
  redirect("/");
}

/**
 * Submits one job and reports the new job id. An empty Idempotency key gets a fresh UUID;
 * re-using a key returns the original job instead of creating a duplicate (docs/05 §9).
 */
export async function submitJob(_prev: FormState, form: FormData): Promise<FormState> {
  const { client } = await api();
  const [taskType = "", versionText = ""] = String(form.get("task") ?? "").split("@");
  const types = await client.GET("/v1/task-types");
  if (types.response.status === 401) redirect("/logout");
  const type = types.data?.items.find((t) => t.task_type === taskType && String(t.task_version) === versionText);
  if (!type) return { error: "Choose a task type." };

  let payload: Record<string, unknown>;
  if (form.get("mode") === "json") {
    try {
      payload = JSON.parse(String(form.get("payload_json") ?? ""));
    } catch {
      return { error: "Payload is not valid JSON." };
    }
  } else {
    payload = payloadFromForm(fieldsFromSchema(type.payload_schema) ?? [], form);
  }

  const options: { max_attempts?: number; timeout_seconds?: number; run_at?: string } = {};
  for (const name of ["max_attempts", "timeout_seconds"] as const) {
    const value = String(form.get(name) ?? "");
    if (value !== "") options[name] = Number(value);
  }
  const delay = Number(form.get("delay_seconds") || 0);
  if (!Number.isFinite(delay) || delay < 0) return { error: "Delay must be 0 or more seconds." };
  if (delay > 0) options.run_at = new Date(Date.now() + delay * 1000).toISOString();

  const key = String(form.get("idempotency_key") ?? "").trim() || crypto.randomUUID();
  const res = await client.POST("/v1/jobs", {
    body: { task_type: type.task_type, task_version: type.task_version, payload, ...options },
    params: { header: { "Idempotency-Key": key } },
  });
  if (res.response.status === 401) redirect("/logout");
  if (!res.data) return problemState(res.error, "Submit failed.");
  return { jobId: res.data.id, replayed: res.response.headers.get("Idempotent-Replayed") === "true" };
}

export type BatchState = { submitted?: number; failed?: number; seconds?: number; error?: string };

/** Submits a mixed batch (lib/batch.ts) with bounded concurrency so the API and DB aren't flooded. */
export async function submitBatch(_prev: BatchState, form: FormData): Promise<BatchState> {
  const count = Number(form.get("count"));
  if (!(BATCH_SIZES as readonly number[]).includes(count)) return { error: "Unsupported batch size." };
  const { client } = await api();
  const types = await client.GET("/v1/task-types");
  if (types.response.status === 401) redirect("/logout");
  const available = new Set(types.data?.items.map((t) => `${t.task_type}@${t.task_version}`));
  const jobs = buildBatch(count, available);

  const started = performance.now();
  let next = 0;
  let submitted = 0;
  let firstError: string | undefined;
  await Promise.all(
    Array.from({ length: BATCH_CONCURRENCY }, async () => {
      while (next < jobs.length) {
        const body = jobs[next++]!;
        try {
          const res = await client.POST("/v1/jobs", { body });
          if (res.data) submitted++;
          else firstError ??= res.error?.title ?? `HTTP ${res.response.status}`;
        } catch {
          firstError ??= "API unreachable";
        }
      }
    }),
  );
  const state: BatchState = {
    submitted,
    failed: count - submitted,
    seconds: Math.round((performance.now() - started) / 100) / 10,
  };
  if (firstError) state.error = firstError;
  return state;
}

/** Cancels a job (best-effort when running), then reloads its page. */
export async function cancelJob(jobId: string): Promise<void> {
  const { client } = await api();
  const res = await client.POST("/v1/jobs/{job_id}/cancel", { params: { path: { job_id: jobId } } });
  if (res.response.status === 401) redirect("/logout");
  // 409 job_not_cancellable means it finished meanwhile; the reloaded page shows the final state.
  redirect(`/jobs/${jobId}`);
}

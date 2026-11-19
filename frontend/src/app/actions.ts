"use server";

import { redirect } from "next/navigation";

import { account, api, clientFor, type Problem } from "@/lib/api/client";
import { fieldsFromSchema, payloadFromForm } from "@/lib/payload";
import { getSession, saveSession, type Session } from "@/lib/session";

const UNREACHABLE = "Can't reach the TaskPilot API. Is it running?";

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

export type SignInState = { error?: string; email?: string; codeSent?: boolean };

/** Step 1: email a sign-in code. The API answers the same way for every address, invited or not. */
export async function requestCode(_prev: SignInState, form: FormData): Promise<SignInState> {
  const email = String(form.get("email") ?? "").trim();
  try {
    const res = await clientFor().POST("/v1/auth/login-codes", { body: { email } });
    if (!res.data) return { error: res.error?.title ?? "Couldn't send a code.", email };
  } catch {
    return { error: UNREACHABLE, email };
  }
  return { codeSent: true, email };
}

/** Points the session at one application, after the API confirms the user belongs to it. */
async function chooseApp(session: Session, appId: string): Promise<boolean> {
  const res = await clientFor(session.token, appId).GET("/v1/me");
  if (!res.data) return false;
  await saveSession({ ...session, appId, tenantName: res.data.tenant_name, scopes: res.data.scopes });
  return true;
}

/**
 * Step 2: exchange the code for a session. With exactly one application the user lands on its
 * dashboard; otherwise they choose (or create) one first.
 */
export async function verifyCode(prev: SignInState, form: FormData): Promise<SignInState> {
  const email = String(form.get("email") ?? prev.email ?? "").trim();
  const code = String(form.get("code") ?? "").replace(/\s/g, "");
  let session: Session;
  let apps: { id: string }[];
  try {
    const res = await clientFor().POST("/v1/auth/sessions", { body: { email, code } });
    if (!res.data) return { error: res.error?.title ?? "Sign-in failed.", email, codeSent: true };
    session = { token: res.data.token, email: res.data.email, exp: Date.parse(res.data.expires_at), scopes: [] };
    await saveSession(session);
    apps = (await clientFor(session.token).GET("/v1/apps")).data?.items ?? [];
  } catch {
    return { error: UNREACHABLE, email, codeSent: true };
  }
  if (apps.length === 1 && (await chooseApp(session, apps[0]!.id))) redirect("/");
  redirect("/apps");
}

/** Switches the dashboard to another of the user's applications. */
export async function selectApp(appId: string): Promise<void> {
  const session = await getSession();
  if (!session) redirect("/login");
  if (!(await chooseApp(session, appId))) redirect("/apps");
  redirect("/");
}

export type AppState = { error?: string; appName?: string; apiKey?: string };

/** Registers an application, selects it, and shows its first API key once. */
export async function createApp(_prev: AppState, form: FormData): Promise<AppState> {
  const { client, session } = await account();
  const name = String(form.get("name") ?? "").trim();
  const res = await client.POST("/v1/apps", { body: { name } });
  if (res.response.status === 401) redirect("/logout");
  if (!res.data) return { error: res.error?.title ?? "Couldn't create the application." };
  await chooseApp(session, res.data.app.id);
  return { appName: res.data.app.name, apiKey: res.data.api_key.key };
}

export type KeyState = { error?: string; apiKey?: string; name?: string };

/** Creates another API key for the selected application and shows it once. */
export async function createKey(_prev: KeyState, form: FormData): Promise<KeyState> {
  const { client, session } = await api();
  const name = String(form.get("name") ?? "").trim();
  const res = await client.POST("/v1/apps/{app_id}/keys", {
    params: { path: { app_id: session.appId! } },
    body: { name },
  });
  if (res.response.status === 401) redirect("/logout");
  if (!res.data) return { error: res.error?.title ?? "Couldn't create the key." };
  return { apiKey: res.data.key, name: res.data.name };
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

/** Cancels a job (best-effort when running), then reloads its page. */
export async function cancelJob(jobId: string): Promise<void> {
  const { client } = await api();
  const res = await client.POST("/v1/jobs/{job_id}/cancel", { params: { path: { job_id: jobId } } });
  if (res.response.status === 401) redirect("/logout");
  // 409 job_not_cancellable means it finished meanwhile; the reloaded page shows the final state.
  redirect(`/jobs/${jobId}`);
}

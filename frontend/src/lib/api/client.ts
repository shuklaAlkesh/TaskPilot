import "server-only";

import createClient from "openapi-fetch";
import { notFound, redirect } from "next/navigation";

import { getSession, type Session } from "@/lib/session";
import type { components, paths } from "./schema";

export type Job = components["schemas"]["Job"];
export type JobStatus = components["schemas"]["JobStatus"];
export type Attempt = components["schemas"]["Attempt"];
export type TaskType = components["schemas"]["TaskType"];
export type Problem = components["schemas"]["Problem"];
export type Me = components["schemas"]["Me"];
export type Stats = components["schemas"]["Stats"];
export type App = components["schemas"]["App"];
export type ApiKey = components["schemas"]["ApiKey"];
export type NewApiKey = components["schemas"]["NewApiKey"];

const TIMEOUT_MS = 10_000;

function baseUrl(): string {
  const url = process.env.TASKPILOT_API_BASE_URL;
  if (!url) throw new Error("TASKPILOT_API_BASE_URL is not set");
  return url;
}

/**
 * A typed Go API client. `token` is a dashboard session token (or none, for sign-in); `appId` selects
 * the application the job endpoints act on. Never cached; never sent to the browser.
 */
export function clientFor(token?: string, appId?: string) {
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  if (appId) headers["X-TaskPilot-App"] = appId;
  return createClient<paths>({
    baseUrl: baseUrl(),
    headers,
    fetch: (request) => fetch(request, { cache: "no-store", signal: AbortSignal.timeout(TIMEOUT_MS) }),
  });
}

type Client = ReturnType<typeof clientFor>;

/** The signed-in user's client (no application selected). Redirects to /login when signed out. */
export async function account(): Promise<{ client: Client; session: Session }> {
  const session = await getSession();
  if (!session) redirect("/login");
  return { client: clientFor(session.token), session };
}

/**
 * The client for the selected application, plus the session. Redirects to /login when signed out and
 * to /apps when no application is selected yet.
 */
export async function api(): Promise<{ client: Client; session: Session }> {
  const session = await getSession();
  if (!session) redirect("/login");
  if (!session.appId) redirect("/apps");
  return { client: clientFor(session.token, session.appId), session };
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly problem: Problem | undefined,
  ) {
    super(problem?.title ?? `API request failed with status ${status}`);
  }
}

/**
 * Unwraps an openapi-fetch result for page rendering: 401 signs out, 404 renders not-found,
 * anything else unexpected throws to the route's error boundary.
 */
export function unwrap<T>(res: { data?: T; error?: unknown; response: Response }): T {
  if (res.response.status === 401) redirect("/logout");
  // The selected application is gone or the user was removed from it: choose another.
  if (res.response.status === 404 && (res.error as Problem | undefined)?.code === "app_not_found") redirect("/apps");
  if (res.response.status === 404) notFound();
  if (res.data === undefined) throw new ApiError(res.response.status, res.error as Problem | undefined);
  return res.data;
}

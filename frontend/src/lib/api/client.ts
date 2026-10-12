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

const TIMEOUT_MS = 10_000;

function baseUrl(): string {
  const url = process.env.TASKPILOT_API_BASE_URL;
  if (!url) throw new Error("TASKPILOT_API_BASE_URL is not set");
  return url;
}

/** A typed Go API client authenticated with the given key. Never cached; never sent to the browser. */
export function clientFor(apiKey: string) {
  return createClient<paths>({
    baseUrl: baseUrl(),
    headers: { Authorization: `Bearer ${apiKey}` },
    fetch: (request) => fetch(request, { cache: "no-store", signal: AbortSignal.timeout(TIMEOUT_MS) }),
  });
}

/** The client for the signed-in tenant, plus its session. Redirects to /login when signed out. */
export async function api(): Promise<{ client: ReturnType<typeof clientFor>; session: Session }> {
  const session = await getSession();
  if (!session) redirect("/login");
  return { client: clientFor(session.apiKey), session };
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
  if (res.response.status === 404) notFound();
  if (res.data === undefined) throw new ApiError(res.response.status, res.error as Problem | undefined);
  return res.data;
}

import { NextResponse, type NextRequest } from "next/server";

import { clientFor, type JobStatus } from "@/lib/api/client";
import { getSession } from "@/lib/session";

const STATUSES = new Set<string>(["queued", "running", "succeeded", "failed", "cancelled"]);

// Read-only list proxy for the dashboard's stage inspector. The API key stays on the server.
export async function GET(request: NextRequest) {
  const session = await getSession();
  if (!session) return NextResponse.json({ code: "unauthenticated" }, { status: 401 });
  const status = request.nextUrl.searchParams.get("status");
  const limit = Math.min(Math.max(Number(request.nextUrl.searchParams.get("limit")) || 10, 1), 100);
  if (status && !STATUSES.has(status)) return NextResponse.json({ code: "invalid_request" }, { status: 400 });
  try {
    const res = await clientFor(session.apiKey).GET("/v1/jobs", {
      params: { query: { limit, ...(status && { status: [status as JobStatus] }) } },
    });
    return NextResponse.json(res.data ?? res.error, {
      status: res.response.status,
      headers: { "Cache-Control": "no-store" },
    });
  } catch {
    return NextResponse.json({ code: "unavailable" }, { status: 503 });
  }
}

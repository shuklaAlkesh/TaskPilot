import { NextResponse } from "next/server";

import { clientFor } from "@/lib/api/client";
import { getSession } from "@/lib/session";

// Read-only proxy used by the job status poller. The session token stays on the server.
export async function GET(_request: Request, { params }: { params: Promise<{ id: string }> }) {
  const session = await getSession();
  if (!session?.appId) return NextResponse.json({ code: "unauthenticated" }, { status: 401 });
  const { id } = await params;
  try {
    const res = await clientFor(session.token, session.appId).GET("/v1/jobs/{job_id}", { params: { path: { job_id: id } } });
    const { status, cancel_requested, attempt_count, updated_at } = res.data ?? {};
    return NextResponse.json(res.data ? { status, cancel_requested, attempt_count, updated_at } : res.error, {
      status: res.response.status,
      headers: { "Cache-Control": "no-store" },
    });
  } catch {
    return NextResponse.json({ code: "unavailable" }, { status: 503 });
  }
}

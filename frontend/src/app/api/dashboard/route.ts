import { NextResponse, type NextRequest } from "next/server";

import { clientFor } from "@/lib/api/client";
import { getSession } from "@/lib/session";

const WINDOWS = new Set(["15m", "1h", "6h", "24h"]);

// Read-only proxy for the live dashboard: stats + the newest jobs. The API key stays on the server.
export async function GET(request: NextRequest) {
  const session = await getSession();
  if (!session) return NextResponse.json({ code: "unauthenticated" }, { status: 401 });
  const window = request.nextUrl.searchParams.get("window") ?? "1h";
  if (!WINDOWS.has(window)) return NextResponse.json({ code: "invalid_request" }, { status: 400 });
  const client = clientFor(session.apiKey);
  try {
    const [stats, recent] = await Promise.all([
      client.GET("/v1/stats", { params: { query: { window: window as "1h" } } }),
      client.GET("/v1/jobs", { params: { query: { limit: 8 } } }),
    ]);
    if (!stats.data || !recent.data) {
      const status = stats.data ? recent.response.status : stats.response.status;
      return NextResponse.json({ code: "upstream_error" }, { status: status === 401 ? 401 : 502 });
    }
    return NextResponse.json({ stats: stats.data, recent: recent.data.items }, { headers: { "Cache-Control": "no-store" } });
  } catch {
    return NextResponse.json({ code: "unavailable" }, { status: 503 });
  }
}

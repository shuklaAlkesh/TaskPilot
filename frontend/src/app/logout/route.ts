import { NextResponse, type NextRequest } from "next/server";

import { clientFor } from "@/lib/api/client";
import { endSession, getSession } from "@/lib/session";

// POST: the "Sign out" button. GET: where pages send the browser when the API answers 401.
async function signOut(request: NextRequest) {
  const session = await getSession();
  if (session) {
    // Revoke the Go session too, so the token is useless even if the cookie leaked. Best-effort.
    await clientFor(session.token).DELETE("/v1/auth/sessions/current").catch(() => undefined);
  }
  await endSession();
  return NextResponse.redirect(new URL("/login", request.url), 303);
}

export const GET = signOut;
export const POST = signOut;

import { NextResponse, type NextRequest } from "next/server";

import { endSession } from "@/lib/session";

// POST: the "Sign out" button. GET: where pages send the browser when the API answers 401.
async function signOut(request: NextRequest) {
  await endSession();
  return NextResponse.redirect(new URL("/login", request.url), 303);
}

export const GET = signOut;
export const POST = signOut;

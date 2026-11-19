import "server-only";

import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";
import { cookies } from "next/headers";

import { SESSION_COOKIE } from "@/lib/session-cookie";

// BFF session (docs/16-FRONTEND.md §4): the dashboard session token from the Go API (issued after email
// sign-in) lives only inside this encrypted, HttpOnly cookie, with the application the user is viewing.
// API keys are never stored here: they are only for application-to-TaskPilot calls.
const IV_BYTES = 12;
const TAG_BYTES = 16;

export type Session = {
  token: string; // tps_… dashboard session token
  email: string;
  exp: number; // epoch milliseconds, the Go session's expiry
  appId?: string; // the selected application (tenant)
  tenantName?: string;
  scopes: string[]; // what the user may do on the selected application ([] until one is selected)
};

function secretKey(): Buffer {
  const key = Buffer.from(process.env.SESSION_SECRET ?? "", "base64");
  if (key.length !== 32) {
    throw new Error("SESSION_SECRET must be 32 random bytes, base64-encoded");
  }
  return key;
}

/** Encrypts a session as base64url(iv | ciphertext | tag) with AES-256-GCM. */
export function seal(session: Session, key: Buffer = secretKey()): string {
  const iv = randomBytes(IV_BYTES);
  const cipher = createCipheriv("aes-256-gcm", key, iv);
  const body = Buffer.concat([cipher.update(JSON.stringify(session), "utf8"), cipher.final()]);
  return Buffer.concat([iv, body, cipher.getAuthTag()]).toString("base64url");
}

/** Decrypts a sealed session. Tampered, malformed, or expired values return null (signed out). */
export function unseal(value: string, key: Buffer = secretKey(), now = Date.now()): Session | null {
  try {
    const raw = Buffer.from(value, "base64url");
    const decipher = createDecipheriv("aes-256-gcm", key, raw.subarray(0, IV_BYTES));
    decipher.setAuthTag(raw.subarray(raw.length - TAG_BYTES));
    const json = Buffer.concat([
      decipher.update(raw.subarray(IV_BYTES, raw.length - TAG_BYTES)),
      decipher.final(),
    ]).toString("utf8");
    const session = JSON.parse(json) as Session;
    return session.exp > now ? session : null;
  } catch {
    return null;
  }
}

export async function getSession(): Promise<Session | null> {
  const value = (await cookies()).get(SESSION_COOKIE)?.value;
  return value ? unseal(value) : null;
}

/** Writes the session cookie; it expires together with the Go session. */
export async function saveSession(session: Session) {
  (await cookies()).set(SESSION_COOKIE, seal(session), {
    httpOnly: true,
    // Browsers treat http://localhost as secure, but `next dev` users may use a LAN IP.
    secure: process.env.NODE_ENV === "production",
    sameSite: "strict",
    path: "/",
    maxAge: Math.max(0, Math.floor((session.exp - Date.now()) / 1000)),
  });
}

export async function endSession() {
  (await cookies()).delete(SESSION_COOKIE);
}

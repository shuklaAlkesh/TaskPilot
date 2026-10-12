import { randomBytes } from "node:crypto";
import { describe, expect, it } from "vitest";

import { seal, unseal, type Session } from "./session";

const key = randomBytes(32);
const session: Session = { apiKey: "tp_abcdefghijkl_secret", tenantName: "demo", scopes: ["jobs:read"], exp: Date.now() + 60_000 };

describe("session cookie", () => {
  it("round-trips and never contains the key in clear text", () => {
    const sealed = seal(session, key);
    expect(sealed).not.toContain("tp_");
    expect(unseal(sealed, key)).toEqual(session);
  });

  it("uses a fresh IV for every seal", () => {
    expect(seal(session, key)).not.toEqual(seal(session, key));
  });

  it("rejects tampering, the wrong key, garbage, and expiry", () => {
    const sealed = seal(session, key);
    const tampered = Buffer.from(sealed, "base64url");
    tampered[20] = (tampered[20] ?? 0) ^ 1;
    expect(unseal(tampered.toString("base64url"), key)).toBeNull();
    expect(unseal(sealed, randomBytes(32))).toBeNull();
    expect(unseal("not-a-cookie", key)).toBeNull();
    expect(unseal(sealed, key, session.exp + 1)).toBeNull();
  });
});

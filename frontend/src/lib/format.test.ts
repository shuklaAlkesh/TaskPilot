import { describe, expect, it } from "vitest";

import { formatCount, formatMs, formatPercent, isTerminal, shortId, timeAgo } from "./format";

describe("format", () => {
  const now = Date.parse("2026-10-12T12:00:00Z");

  it("formats relative times with Intl", () => {
    expect(timeAgo("2026-10-12T11:57:00Z", now)).toBe("3 minutes ago");
    expect(timeAgo("2026-10-12T12:00:00Z", now)).toBe("now");
    expect(timeAgo("2026-10-12T14:00:00Z", now)).toBe("in 2 hours");
  });

  it("shortens ids and detects terminal states", () => {
    expect(shortId("0192f3c4-7d1e-7c55-9a0b-1f5e2d3c4b5a")).toBe("2d3c4b5a");
    expect(isTerminal("failed")).toBe(true);
    expect(isTerminal("running")).toBe(false);
  });

  it("formats counts, durations, and rates for the dashboard", () => {
    expect(formatCount(1284)).toBe("1,284");
    expect(formatCount(12_900)).toBe("12.9K");
    expect(formatMs(null)).toBe("—");
    expect(formatMs(412.4)).toBe("412 ms");
    expect(formatMs(2345)).toBe("2.35 s");
    expect(formatMs(90_000)).toBe("1.5 min");
    expect(formatPercent(0.9937)).toBe("99.4%");
    expect(formatPercent(null)).toBe("—");
  });
});

import { describe, expect, it } from "vitest";

import { buildBatch } from "./batch";

function seeded(seed: number) {
  return () => {
    seed = (seed * 1664525 + 1013904223) % 2 ** 32;
    return seed / 2 ** 32;
  };
}

const ALL = new Set(["system.echo@1", "system.sleep@1", "text.word_count@1", "data.csv_summary@1"]);

describe("buildBatch", () => {
  it("builds the requested number of registered jobs in roughly the documented mix", () => {
    const jobs = buildBatch(10_000, ALL, seeded(42));
    expect(jobs).toHaveLength(10_000);
    const share = (t: string) => jobs.filter((j) => j.task_type === t).length / jobs.length;
    expect(share("system.echo")).toBeCloseTo(0.4, 1);
    expect(share("text.word_count")).toBeCloseTo(0.25, 1);
    expect(share("data.csv_summary")).toBeCloseTo(0.15, 1);
    expect(share("system.sleep")).toBeCloseTo(0.2, 1);
    for (const j of jobs) expect(ALL.has(`${j.task_type}@${j.task_version}`)).toBe(true);
  });

  it("keeps sleep payloads inside the schema and includes every outcome", () => {
    const sleeps = buildBatch(5_000, ALL, seeded(7)).filter((j) => j.task_type === "system.sleep");
    const outcomes = new Set(sleeps.map((j) => String(j.payload.outcome ?? "succeed")));
    expect(outcomes).toEqual(new Set(["succeed", "retryable_error", "permanent_error"]));
    for (const { payload } of sleeps) {
      expect(payload.seconds).toBeGreaterThanOrEqual(0);
      expect(payload.seconds).toBeLessThanOrEqual(0.4);
    }
  });

  it("never uses test-only tasks when they are disabled", () => {
    const jobs = buildBatch(2_000, new Set(["system.echo@1", "text.word_count@1", "data.csv_summary@1"]), seeded(1));
    expect(jobs.some((j) => j.task_type === "system.sleep")).toBe(false);
  });
});

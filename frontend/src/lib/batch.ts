// Mixed workload for the dashboard's batch submit. Every job is a registered task type with a
// schema-valid payload; test-only system.sleep jobs inject retries and dead letters so the
// pipeline shows every path (docs/05-JOB-LIFECYCLE.md §3).

export const BATCH_SIZES = [1000, 5000, 10000] as const;
export type BatchSize = (typeof BATCH_SIZES)[number];

export type BatchJob = {
  task_type: string;
  task_version: number;
  payload: Record<string, unknown>;
};

const TEXTS = [
  "the quick brown fox jumps over the lazy dog",
  "to be or not to be that is the question",
  "jobs are retried with backoff until they succeed or run out of attempts",
];
const CSVS = ["city,temp\nPune,31\nDelhi,35\nAgra,33", "item,qty,price\npen,3,10.5\nbook,1,250\nbag,2,899"];

/** The sleep outcome mix: most succeed, some succeed after retries, a few dead-letter. */
function sleepPayload(random: () => number): Record<string, unknown> {
  const seconds = Math.round(random() * 40) / 100; // 0–0.4 s keeps batches quick
  const r = random();
  if (r < 0.6) return { seconds };
  if (r < 0.9) return { seconds, outcome: "retryable_error", fail_until_attempt: r < 0.8 ? 2 : 3 };
  if (r < 0.97) return { seconds, outcome: "retryable_error" }; // exhausts max_attempts
  return { seconds, outcome: "permanent_error" };
}

/**
 * Builds `count` jobs: 40% echo, 25% word count, 15% CSV summary, 20% sleep (when enabled).
 * `available` holds "task_type@version" keys from GET /v1/task-types.
 */
export function buildBatch(count: number, available: Set<string>, random: () => number = Math.random): BatchJob[] {
  const withSleep = available.has("system.sleep@1");
  const pick = <T,>(items: readonly T[]) => items[Math.floor(random() * items.length)] as T;
  return Array.from({ length: count }, (_, i): BatchJob => {
    const r = random();
    if (withSleep && r >= 0.8) return { task_type: "system.sleep", task_version: 1, payload: sleepPayload(random) };
    if (r < 0.4 || (!withSleep && r >= 0.8)) {
      return { task_type: "system.echo", task_version: 1, payload: { message: `batch job ${i + 1}` } };
    }
    if (r < 0.65) return { task_type: "text.word_count", task_version: 1, payload: { text: pick(TEXTS), top_n: 3 } };
    return { task_type: "data.csv_summary", task_version: 1, payload: { csv: pick(CSVS) } };
  });
}

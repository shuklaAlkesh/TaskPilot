"use client";

import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

const FAST_MS = 2_000;
const SLOW_MS = 10_000;
const SLOW_AFTER_MS = 60_000;

/**
 * Polls /api/jobs/[id] while the job is not terminal and refreshes the server-rendered page when
 * anything changes. Backs off after a minute and pauses while the tab is hidden (docs/16 §5).
 */
export function JobStatusPoller({ jobId }: { jobId: string }) {
  const router = useRouter();
  const [live, setLive] = useState(true);

  useEffect(() => {
    const started = Date.now();
    const controller = new AbortController();
    let last = "";
    let timer: ReturnType<typeof setTimeout> | undefined;

    const schedule = () => {
      timer = setTimeout(tick, Date.now() - started > SLOW_AFTER_MS ? SLOW_MS : FAST_MS);
    };
    const tick = async () => {
      if (document.visibilityState === "hidden") return; // resumed by the visibilitychange listener
      try {
        const res = await fetch(`/api/jobs/${jobId}`, { signal: controller.signal, cache: "no-store" });
        if (res.status === 401) {
          router.refresh(); // the server render sees the 401 and signs out
          return;
        }
        if (res.ok) {
          const snapshot = JSON.stringify(await res.json());
          if (last && snapshot !== last) router.refresh();
          last = snapshot;
        }
      } catch {
        if (controller.signal.aborted) return;
      }
      schedule();
    };
    const onVisible = () => {
      setLive(document.visibilityState === "visible");
      if (document.visibilityState === "visible") {
        clearTimeout(timer);
        void tick();
      }
    };

    document.addEventListener("visibilitychange", onVisible);
    void tick();
    return () => {
      controller.abort();
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [jobId, router]);

  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-zinc-500" role="status">
      <span className={`size-2 rounded-full ${live ? "animate-pulse bg-sky-500" : "bg-zinc-400"}`} aria-hidden="true" />
      {live ? "Live" : "Paused"}
    </span>
  );
}

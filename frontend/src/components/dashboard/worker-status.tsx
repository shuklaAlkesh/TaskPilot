/** Whether any worker can run this task right now; with none, jobs wait in the queue. */
export function WorkerStatus({ online }: { online: number }) {
  return online > 0 ? (
    <span className="flex items-center gap-1.5 text-emerald-300">
      <span className="size-1.5 rounded-full bg-emerald-400" aria-hidden="true" />
      {online} worker{online === 1 ? "" : "s"} online
    </span>
  ) : (
    <span className="flex items-center gap-1.5 text-amber-300">
      <span className="size-1.5 rounded-full bg-amber-400" aria-hidden="true" />
      no worker online: jobs will wait
    </span>
  );
}

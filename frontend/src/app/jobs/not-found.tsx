import Link from "next/link";

export default function JobNotFound() {
  return (
    <div className="py-16 text-center">
      <h1 className="text-xl font-semibold">Job not found</h1>
      <p className="mt-2 text-zinc-500">It may have been deleted by retention, or it belongs to another tenant.</p>
      <Link href="/jobs" className="mt-6 inline-block text-indigo-600 hover:underline dark:text-indigo-400">
        Back to jobs
      </Link>
    </div>
  );
}

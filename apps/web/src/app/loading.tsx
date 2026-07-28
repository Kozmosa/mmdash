import { LoadingState } from "@/components/states/loading-state";

export default function Loading() {
  return (
    <main className="mx-auto w-full max-w-7xl p-6 lg:p-10">
      <LoadingState label="正在加载 mmdash…" />
    </main>
  );
}

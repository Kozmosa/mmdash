import { ListChecks } from "lucide-react";

import { FeaturePlaceholder } from "@/components/states/feature-placeholder";

export default function ProgressPage() {
  return (
    <FeaturePlaceholder
      description="关键节点、任务、看板、甘特、今日视图与 Proposal"
      icon={ListChecks}
      title="进度跟踪"
    />
  );
}

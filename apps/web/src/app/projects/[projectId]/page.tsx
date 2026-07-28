import { Gauge } from "lucide-react";

import { FeaturePlaceholder } from "@/components/states/feature-placeholder";

export default function ProjectHomePage() {
  return (
    <FeaturePlaceholder
      description="题目、关键节点、任务、模型、实验和论文的统一摘要入口"
      icon={Gauge}
      title="首页"
    />
  );
}

import { Waypoints } from "lucide-react";

import { FeaturePlaceholder } from "@/components/states/feature-placeholder";

export default function ModelsPage() {
  return (
    <FeaturePlaceholder
      description="模型来源、版本时间线、渲染与版本差异"
      icon={Waypoints}
      title="模型版本"
    />
  );
}

import { FlaskConical } from "lucide-react";

import { FeaturePlaceholder } from "@/components/states/feature-placeholder";

export default function ExperimentsPage() {
  return (
    <FeaturePlaceholder
      description="实验状态、参数、日志、结果预览、比较与 Box 状态"
      icon={FlaskConical}
      title="求解记录"
    />
  );
}

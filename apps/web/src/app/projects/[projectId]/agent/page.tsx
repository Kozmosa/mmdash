import { Bot } from "lucide-react";

import { FeaturePlaceholder } from "@/components/states/feature-placeholder";

export default function AgentPage() {
  return (
    <FeaturePlaceholder
      description="Hermes Agent 会话、运行状态与自动进度入口"
      icon={Bot}
      title="mmdash Agent"
    />
  );
}

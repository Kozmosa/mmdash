import { FilePenLine } from "lucide-react";

import { FeaturePlaceholder } from "@/components/states/feature-placeholder";

export default function ArticlePage() {
  return (
    <FeaturePlaceholder
      description="参考区、块级 Markdown 编辑、LaTeX 构建与 Release"
      icon={FilePenLine}
      title="论文写作"
    />
  );
}

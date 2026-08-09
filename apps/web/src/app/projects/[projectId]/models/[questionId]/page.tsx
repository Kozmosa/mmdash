import { ModelQuestionPage } from "@/features/model/model-question-page";

export default async function ModelDetailPage({ params }: { params: Promise<{ questionId: string }> }) {
  const { questionId } = await params;
  return <ModelQuestionPage questionId={questionId} />;
}

import { ExperimentDetailWorkbench } from "@/features/experiment/experiment-detail-workbench";

type ExperimentDetailPageProps = {
  params: Promise<{ experimentId: string }>;
};

export default async function ExperimentDetailPage({ params }: Readonly<ExperimentDetailPageProps>) {
  const { experimentId } = await params;
  return <ExperimentDetailWorkbench experimentId={experimentId} />;
}

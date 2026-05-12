import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import DashboardLayout from "@/components/DashboardLayout";

export default async function MainLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const cookieStore = await cookies();
  const token = cookieStore.get("mmdash_token")?.value;
  if (!token) {
    redirect("/auth/login");
  }
  return <DashboardLayout>{children}</DashboardLayout>;
}

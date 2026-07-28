import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import type { SettingsSlot } from "@/features/settings/registry";

export function SettingsSlotGrid({
  slots,
}: Readonly<{ slots: readonly SettingsSlot[] }>) {
  return (
    <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
      {slots.map((slot) => (
        <Card className="min-h-40" key={slot.id}>
          <CardHeader>
            <CardTitle className="text-base">{slot.title}</CardTitle>
            <CardDescription>{slot.description}</CardDescription>
          </CardHeader>
          <CardContent>
            <p className="text-xs text-muted-foreground">
              等待 <code>{slot.owner}</code> 模块注册设置面板
            </p>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

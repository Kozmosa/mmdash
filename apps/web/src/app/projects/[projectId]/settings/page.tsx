import { Settings } from "lucide-react";

import { SettingsSlotGrid } from "@/features/settings/settings-slot-grid";
import { RegisteredSettingsPanel } from "@/features/settings/registered-settings-panel";
import { settingsSlots } from "@/features/settings/registry";
import { MemberManagement } from "@/features/members/member-management";

export default function SettingsPage() {
  return (
    <section className="space-y-6" aria-labelledby="settings-title">
      <header>
        <div className="mb-3 flex size-10 items-center justify-center rounded-lg border border-border bg-card shadow-xs">
          <Settings aria-hidden="true" className="size-5" />
        </div>
        <h1
          className="text-2xl font-semibold tracking-tight"
          id="settings-title"
        >
          设置
        </h1>
        <p className="mt-1 text-sm text-muted-foreground">
          各领域模块通过稳定插槽注册自己的项目设置
        </p>
      </header>
      <SettingsSlotGrid slots={settingsSlots.list()} />
      <MemberManagement />
      <section
        className="space-y-3"
        aria-labelledby="registered-settings-title"
      >
        <div>
          <h2 className="text-lg font-semibold" id="registered-settings-title">
            已注册配置类型
          </h2>
          <p className="text-sm text-muted-foreground">
            字段契约、密钥标记和连接测试能力由所属模块注册
          </p>
        </div>
        <RegisteredSettingsPanel />
      </section>
    </section>
  );
}

import { describe, expect, it } from "vitest";

import { SettingsSlotRegistry } from "@/features/settings/registry";

describe("settings slot registry", () => {
  it("orders slots and rejects duplicate ids", () => {
    const registry = new SettingsSlotRegistry();
    registry.register({
      id: "later",
      title: "Later",
      description: "Later slot",
      owner: "test",
      order: 20,
    });
    registry.register({
      id: "first",
      title: "First",
      description: "First slot",
      owner: "test",
      order: 10,
    });

    expect(registry.list().map((slot) => slot.id)).toEqual(["first", "later"]);
    expect(() =>
      registry.register({
        id: "first",
        title: "Duplicate",
        description: "Duplicate slot",
        owner: "test",
        order: 30,
      }),
    ).toThrow('Settings slot "first" is already registered');
  });
});

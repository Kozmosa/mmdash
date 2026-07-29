"use client";

import { useEffect, useState } from "react";

import { cn } from "@/lib/cn";

export function UserAvatar({
  displayName,
  email,
  className,
}: {
  displayName?: string;
  email?: string;
  className?: string;
}) {
  const [url, setUrl] = useState<string>();
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let active = true;
    if (!email) {
      setUrl(undefined);
      return;
    }
    void crypto.subtle
      .digest("SHA-256", new TextEncoder().encode(email.trim().toLowerCase()))
      .then((buffer) => {
        if (active)
          setUrl(
            `https://www.gravatar.com/avatar/${[...new Uint8Array(buffer)].map((value) => value.toString(16).padStart(2, "0")).join("")}?d=404&s=96`,
          );
      });
    return () => {
      active = false;
    };
  }, [email]);
  return (
    <span
      className={cn(
        "flex size-7 items-center justify-center overflow-hidden rounded-lg bg-primary text-xs font-semibold text-primary-foreground",
        className,
      )}
    >
      {url && !failed ? (
        <img
          alt=""
          className="size-full object-cover"
          onError={() => setFailed(true)}
          src={url}
        />
      ) : (
        (displayName ?? "·").slice(0, 1).toUpperCase()
      )}
    </span>
  );
}

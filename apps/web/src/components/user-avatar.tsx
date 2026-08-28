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
  const [loaded, setLoaded] = useState(false);
  const [fallbackToCravatar, setFallbackToCravatar] = useState(false);
  useEffect(() => {
    let active = true;
    setFailed(false);
    setLoaded(false);
    setUrl(undefined);
    setFallbackToCravatar(false);
    if (!email) {
      return;
    }
    void crypto.subtle
      .digest("SHA-256", new TextEncoder().encode(email.trim().toLowerCase()))
      .then((buffer) => {
        if (active)
          setUrl(
            `https://www.gravatar.com/avatar/${[...new Uint8Array(buffer)].map((value) => value.toString(16).padStart(2, "0")).join("")}?d=404&s=400`,
          );
      });
    return () => {
      active = false;
    };
  }, [email]);
  return (
    <span
      className={cn(
        "relative flex size-7 items-center justify-center overflow-hidden rounded-lg text-xs font-semibold text-foreground shadow-[0_3px_10px_rgba(0,0,0,0.10)]",
        className,
      )}
    >
      <span aria-hidden="true">
        {(displayName ?? "·").slice(0, 1).toUpperCase()}
      </span>
      {url && !failed ? (
        <img
          alt=""
          className={cn(
            "absolute inset-0 block size-full scale-[1.04] object-cover transition-opacity duration-150",
            loaded ? "opacity-100" : "opacity-0",
          )}
          onError={() => {
            if (url.includes("gravatar.com") && !fallbackToCravatar) {
              setFallbackToCravatar(true);
              setLoaded(false);
              setUrl(url.replace("www.gravatar.com", "cravatar.cn"));
            } else {
              setFailed(true);
              setLoaded(false);
            }
          }}
          onLoad={() => setLoaded(true)}
          src={url}
        />
      ) : null}
    </span>
  );
}

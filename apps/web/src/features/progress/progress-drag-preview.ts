import type { DragEvent } from "react";

export function beginNativeProgressDrag(
  event: DragEvent<HTMLElement>,
  dataType: string,
  id: string,
) {
  event.dataTransfer.setData(dataType, id);
  event.dataTransfer.effectAllowed = "move";

  const source = event.currentTarget;
  const rectangle = source.getBoundingClientRect();
  const preview = source.cloneNode(true) as HTMLElement;
  preview.setAttribute("aria-hidden", "true");
  preview.style.position = "fixed";
  preview.style.left = "-10000px";
  preview.style.top = "-10000px";
  preview.style.width = `${rectangle.width}px`;
  preview.style.opacity = "0.68";
  preview.style.pointerEvents = "none";
  preview.style.transform = "rotate(1deg) scale(0.98)";
  preview.style.boxShadow = "0 18px 48px rgb(15 23 42 / 0.24)";
  preview.style.zIndex = "9999";
  document.body.append(preview);
  event.dataTransfer.setDragImage(
    preview,
    Math.max(12, Math.min(rectangle.width / 2, event.clientX - rectangle.left)),
    Math.max(12, Math.min(rectangle.height / 2, event.clientY - rectangle.top)),
  );
  source.style.opacity = "0.35";
  window.setTimeout(() => preview.remove(), 0);
}

export function endNativeProgressDrag(event: DragEvent<HTMLElement>) {
  event.currentTarget.style.opacity = "";
}

"""Strictly bounded, deterministic Artifact preview processors."""

from __future__ import annotations

import csv
import io
import json
import math
import os
import time
import warnings
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import pymupdf
from PIL import Image, ImageOps, UnidentifiedImageError


class PreviewFailure(RuntimeError):
    """A safe, non-retryable preview processing failure."""

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code


class PreviewUnsupported(RuntimeError):
    """A bounded input that has no safe Stage 2 preview."""

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


@dataclass(frozen=True)
class PreviewConfig:
    """System limits for untrusted preview input and output."""

    max_input_bytes: int = 128 * 1024 * 1024
    max_image_pixels: int = 40_000_000
    max_pdf_pages: int = 1_000
    max_pdf_text_pages: int = 20
    max_csv_rows: int = 10_000
    max_csv_columns: int = 256
    max_sample_rows: int = 20
    max_json_bytes: int = 8 * 1024 * 1024
    max_text_bytes: int = 2 * 1024 * 1024
    max_text_chars: int = 32_768
    max_summary_bytes: int = 64 * 1024
    max_thumbnail_bytes: int = 4 * 1024 * 1024
    thumbnail_dimension: int = 512
    timeout_seconds: float = 60.0

    @classmethod
    def from_environment(cls) -> PreviewConfig:
        """Load positive system limits without exposing them as Project settings."""

        defaults = cls()
        return cls(
            max_input_bytes=_positive_int(
                "MMDASH_PREVIEW_MAX_INPUT_BYTES", defaults.max_input_bytes
            ),
            max_image_pixels=_positive_int(
                "MMDASH_PREVIEW_MAX_IMAGE_PIXELS", defaults.max_image_pixels
            ),
            max_pdf_pages=_positive_int("MMDASH_PREVIEW_MAX_PDF_PAGES", defaults.max_pdf_pages),
            max_pdf_text_pages=_positive_int(
                "MMDASH_PREVIEW_MAX_PDF_TEXT_PAGES", defaults.max_pdf_text_pages
            ),
            max_csv_rows=_positive_int("MMDASH_PREVIEW_MAX_CSV_ROWS", defaults.max_csv_rows),
            max_csv_columns=_positive_int(
                "MMDASH_PREVIEW_MAX_CSV_COLUMNS", defaults.max_csv_columns
            ),
            max_sample_rows=_positive_int(
                "MMDASH_PREVIEW_MAX_SAMPLE_ROWS", defaults.max_sample_rows
            ),
            max_json_bytes=_positive_int("MMDASH_PREVIEW_MAX_JSON_BYTES", defaults.max_json_bytes),
            max_text_bytes=_positive_int("MMDASH_PREVIEW_MAX_TEXT_BYTES", defaults.max_text_bytes),
            max_text_chars=_positive_int("MMDASH_PREVIEW_MAX_TEXT_CHARS", defaults.max_text_chars),
            max_summary_bytes=_positive_int(
                "MMDASH_PREVIEW_MAX_SUMMARY_BYTES", defaults.max_summary_bytes
            ),
            max_thumbnail_bytes=_positive_int(
                "MMDASH_PREVIEW_MAX_THUMBNAIL_BYTES", defaults.max_thumbnail_bytes
            ),
            thumbnail_dimension=_positive_int(
                "MMDASH_PREVIEW_THUMBNAIL_DIMENSION", defaults.thumbnail_dimension
            ),
            timeout_seconds=_positive_float(
                "MMDASH_PREVIEW_TIMEOUT_SECONDS", defaults.timeout_seconds
            ),
        )


@dataclass(frozen=True)
class Thumbnail:
    content: bytes
    filename: str
    mime_type: str


@dataclass(frozen=True)
class ProcessedPreview:
    status: str
    structural_summary: dict[str, Any]
    error_code: str | None = None
    thumbnail: Thumbnail | None = None


class PreviewProcessor:
    """Generates only format metadata, bounded samples, and safe thumbnails."""

    def __init__(self, config: PreviewConfig | None = None) -> None:
        self.config = config or PreviewConfig()

    def process(self, path: Path, preview_type: str) -> ProcessedPreview:
        started = time.monotonic()
        try:
            if path.stat().st_size > self.config.max_input_bytes:
                raise PreviewUnsupported("input_too_large")
            if preview_type == "image":
                result = self._image(path, started)
            elif preview_type == "pdf":
                result = self._pdf(path, started)
            elif preview_type == "csv":
                result = self._csv(path, started)
            elif preview_type == "json":
                result = self._json(path, started)
            elif preview_type == "text":
                result = self._text(path, started)
            else:
                raise PreviewUnsupported("format_not_supported")
            self._validate_summary(result.structural_summary)
            return result
        except PreviewUnsupported as error:
            return ProcessedPreview(
                status="unsupported",
                structural_summary={"reason": error.reason},
                error_code="ARTIFACT_PREVIEW_UNSUPPORTED",
            )

    def _image(self, path: Path, started: float) -> ProcessedPreview:
        previous_limit = Image.MAX_IMAGE_PIXELS
        Image.MAX_IMAGE_PIXELS = self.config.max_image_pixels
        try:
            with warnings.catch_warnings():
                warnings.simplefilter("error", Image.DecompressionBombWarning)
                with Image.open(path) as source:
                    source.load()
                    self._check_deadline(started)
                    width, height = source.size
                    if width * height > self.config.max_image_pixels:
                        raise PreviewUnsupported("image_pixel_limit")
                    frames = int(getattr(source, "n_frames", 1))
                    image_format = (source.format or "unknown").lower()
                    thumbnail = ImageOps.exif_transpose(source.copy())
                    thumbnail.seek(0)
                    thumbnail.thumbnail(
                        (self.config.thumbnail_dimension, self.config.thumbnail_dimension),
                        Image.Resampling.LANCZOS,
                    )
                    thumbnail_bytes, mime_type, suffix = self._encode_thumbnail(thumbnail)
                    return ProcessedPreview(
                        status="available",
                        structural_summary={
                            "format": image_format,
                            "width": width,
                            "height": height,
                            "frames": frames,
                            "mode": source.mode,
                        },
                        thumbnail=Thumbnail(
                            content=thumbnail_bytes,
                            filename=f"thumbnail.{suffix}",
                            mime_type=mime_type,
                        ),
                    )
        except (UnidentifiedImageError, OSError, ValueError) as error:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_INVALID_IMAGE",
                "Image preview input is invalid",
            ) from error
        finally:
            Image.MAX_IMAGE_PIXELS = previous_limit

    def _pdf(self, path: Path, started: float) -> ProcessedPreview:
        try:
            with pymupdf.open(path) as document:
                page_count = document.page_count
                if page_count < 1:
                    raise PreviewFailure(
                        "ARTIFACT_PREVIEW_INVALID_PDF",
                        "PDF preview input has no pages",
                    )
                if page_count > self.config.max_pdf_pages:
                    raise PreviewUnsupported("pdf_page_limit")
                text_parts: list[str] = []
                remaining = self.config.max_text_chars
                for page_number in range(min(page_count, self.config.max_pdf_text_pages)):
                    self._check_deadline(started)
                    text = document.load_page(page_number).get_text("text")
                    if text:
                        bounded = text[:remaining]
                        text_parts.append(bounded)
                        remaining -= len(bounded)
                    if remaining <= 0:
                        break
                first_page = document.load_page(0)
                rectangle = first_page.rect
                largest = max(float(rectangle.width), float(rectangle.height), 1.0)
                scale = min(2.0, self.config.thumbnail_dimension / largest)
                pixmap = first_page.get_pixmap(
                    matrix=pymupdf.Matrix(scale, scale),
                    alpha=False,
                    colorspace=pymupdf.csRGB,
                )
                thumbnail = pixmap.tobytes("png")
                self._check_thumbnail(thumbnail)
                return ProcessedPreview(
                    status="available",
                    structural_summary={
                        "format": "pdf",
                        "page_count": page_count,
                        "text": "".join(text_parts)[: self.config.max_text_chars],
                        "text_pages_scanned": min(page_count, self.config.max_pdf_text_pages),
                    },
                    thumbnail=Thumbnail(
                        content=thumbnail,
                        filename="thumbnail.png",
                        mime_type="image/png",
                    ),
                )
        except PreviewFailure:
            raise
        except PreviewUnsupported:
            raise
        except (pymupdf.FileDataError, RuntimeError, ValueError) as error:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_INVALID_PDF",
                "PDF preview input is invalid",
            ) from error

    def _csv(self, path: Path, started: float) -> ProcessedPreview:
        if path.stat().st_size > self.config.max_text_bytes:
            raise PreviewUnsupported("csv_byte_limit")
        rows_read = 0
        columns: list[str] = []
        column_stats: list[dict[str, Any]] = []
        sample: list[list[str]] = []
        truncated = False
        try:
            with path.open("r", encoding="utf-8-sig", errors="strict", newline="") as source:
                reader = csv.reader(source)
                for row in reader:
                    self._check_deadline(started)
                    if len(row) > self.config.max_csv_columns:
                        raise PreviewUnsupported("csv_column_limit")
                    bounded = [cell[:1024] for cell in row]
                    if rows_read == 0:
                        columns = bounded
                        column_stats = [
                            {
                                "name": name,
                                "non_empty_count": 0,
                                "numeric_count": 0,
                                "numeric_min": None,
                                "numeric_max": None,
                            }
                            for name in columns
                        ]
                    elif len(sample) < self.config.max_sample_rows:
                        sample.append(bounded)
                    if rows_read > 0:
                        for index, cell in enumerate(bounded[: len(column_stats)]):
                            if not cell:
                                continue
                            stats = column_stats[index]
                            stats["non_empty_count"] += 1
                            try:
                                numeric = float(cell)
                            except ValueError:
                                continue
                            if not math.isfinite(numeric):
                                continue
                            stats["numeric_count"] += 1
                            minimum = stats["numeric_min"]
                            maximum = stats["numeric_max"]
                            if minimum is None or numeric < minimum:
                                stats["numeric_min"] = numeric
                            if maximum is None or numeric > maximum:
                                stats["numeric_max"] = numeric
                    rows_read += 1
                    if rows_read > self.config.max_csv_rows:
                        truncated = True
                        break
        except UnicodeDecodeError as error:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_INVALID_ENCODING",
                "CSV preview input is not valid UTF-8",
            ) from error
        except csv.Error as error:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_INVALID_CSV",
                "CSV preview input is invalid",
            ) from error
        return ProcessedPreview(
            status="available",
            structural_summary={
                "encoding": "utf-8",
                "columns": columns,
                "column_count": len(columns),
                "row_count": max(0, rows_read - 1),
                "row_count_truncated": truncated,
                "column_stats": column_stats,
                "sample": sample,
            },
        )

    def _json(self, path: Path, started: float) -> ProcessedPreview:
        if path.stat().st_size > self.config.max_json_bytes:
            raise PreviewUnsupported("json_byte_limit")
        try:
            with path.open("r", encoding="utf-8-sig", errors="strict") as source:
                value = json.load(source, parse_constant=_reject_json_constant)
        except UnicodeDecodeError as error:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_INVALID_ENCODING",
                "JSON preview input is not valid UTF-8",
            ) from error
        except (json.JSONDecodeError, RecursionError, ValueError) as error:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_INVALID_JSON",
                "JSON preview input is invalid",
            ) from error
        self._check_deadline(started)
        summary: dict[str, Any] = {
            "encoding": "utf-8",
            "top_level_type": _json_type(value),
            "sample": _bounded_json(value),
        }
        if isinstance(value, dict):
            summary["top_level_keys"] = [str(key)[:256] for key in list(value)[:100]]
            summary["key_count"] = len(value)
        elif isinstance(value, list):
            summary["item_count"] = len(value)
        return ProcessedPreview(status="available", structural_summary=summary)

    def _text(self, path: Path, started: float) -> ProcessedPreview:
        if path.stat().st_size > self.config.max_text_bytes:
            raise PreviewUnsupported("text_byte_limit")
        raw = path.read_bytes()
        self._check_deadline(started)
        if b"\x00" in raw:
            raise PreviewUnsupported("binary_content")
        try:
            text = raw.decode("utf-8-sig", errors="strict")
        except UnicodeDecodeError:
            raise PreviewUnsupported("invalid_utf8")
        controls = sum(
            1 for character in text if ord(character) < 0x20 and character not in "\n\r\t"
        )
        if text and controls / len(text) > 0.01:
            raise PreviewUnsupported("binary_content")
        return ProcessedPreview(
            status="available",
            structural_summary={
                "encoding": "utf-8",
                "line_count": text.count("\n") + (1 if text else 0),
                "text": text[: self.config.max_text_chars],
                "text_truncated": len(text) > self.config.max_text_chars,
            },
        )

    def _encode_thumbnail(self, image: Image.Image) -> tuple[bytes, str, str]:
        output = io.BytesIO()
        if "A" in image.getbands():
            image.save(output, format="PNG", optimize=True)
            mime_type, suffix = "image/png", "png"
        else:
            image.convert("RGB").save(
                output,
                format="JPEG",
                quality=82,
                optimize=True,
                progressive=False,
            )
            mime_type, suffix = "image/jpeg", "jpg"
        content = output.getvalue()
        self._check_thumbnail(content)
        return content, mime_type, suffix

    def _check_thumbnail(self, content: bytes) -> None:
        if not content or len(content) > self.config.max_thumbnail_bytes:
            raise PreviewUnsupported("thumbnail_byte_limit")

    def _check_deadline(self, started: float) -> None:
        if time.monotonic() - started > self.config.timeout_seconds:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_TIMEOUT",
                "Preview processing exceeded its time limit",
            )

    def _validate_summary(self, summary: dict[str, Any]) -> None:
        encoded = json.dumps(
            summary,
            ensure_ascii=False,
            separators=(",", ":"),
        ).encode("utf-8")
        if len(encoded) > self.config.max_summary_bytes:
            raise PreviewFailure(
                "ARTIFACT_PREVIEW_SUMMARY_TOO_LARGE",
                "Preview structural summary exceeded its byte limit",
            )


def _json_type(value: object) -> str:
    if value is None:
        return "null"
    if isinstance(value, bool):
        return "boolean"
    if isinstance(value, dict):
        return "object"
    if isinstance(value, list):
        return "array"
    if isinstance(value, str):
        return "string"
    if isinstance(value, (int, float)):
        return "number"
    return "unknown"


def _reject_json_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant: {value}")


def _bounded_json(value: Any, *, depth: int = 0) -> Any:
    if depth >= 4:
        return "<truncated>"
    if isinstance(value, dict):
        return {
            str(key)[:256]: _bounded_json(item, depth=depth + 1)
            for key, item in list(value.items())[:20]
        }
    if isinstance(value, list):
        return [_bounded_json(item, depth=depth + 1) for item in value[:20]]
    if isinstance(value, str):
        return value[:1024]
    if value is None or isinstance(value, (bool, int, float)):
        return value
    return str(value)[:1024]


def _positive_int(name: str, default: int) -> int:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError as error:
        raise ValueError(f"{name} must be a positive integer") from error
    if value <= 0:
        raise ValueError(f"{name} must be a positive integer")
    return value


def _positive_float(name: str, default: float) -> float:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        value = float(raw)
    except ValueError as error:
        raise ValueError(f"{name} must be positive") from error
    if value <= 0:
        raise ValueError(f"{name} must be positive")
    return value

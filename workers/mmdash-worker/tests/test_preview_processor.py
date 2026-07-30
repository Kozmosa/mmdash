import io
import json
from pathlib import Path

import pymupdf
import pytest
from PIL import Image

from mmdash_worker.preview.processor import (
    PreviewConfig,
    PreviewFailure,
    PreviewProcessor,
)


@pytest.fixture
def processor() -> PreviewProcessor:
    return PreviewProcessor(
        PreviewConfig(
            max_input_bytes=2 * 1024 * 1024,
            max_image_pixels=1_000_000,
            max_pdf_pages=20,
            max_pdf_text_pages=5,
            max_csv_rows=10,
            max_csv_columns=10,
            max_sample_rows=2,
            max_json_bytes=1024 * 1024,
            max_text_bytes=1024 * 1024,
            max_text_chars=100,
            max_summary_bytes=16 * 1024,
            max_thumbnail_bytes=1024 * 1024,
            thumbnail_dimension=128,
            timeout_seconds=5,
        )
    )


def test_image_preview_has_dimensions_and_safe_thumbnail(
    tmp_path: Path,
    processor: PreviewProcessor,
) -> None:
    path = tmp_path / "image.png"
    Image.new("RGB", (320, 180), (18, 72, 120)).save(path, format="PNG")

    result = processor.process(path, "image")

    assert result.status == "available"
    assert result.structural_summary == {
        "format": "png",
        "width": 320,
        "height": 180,
        "frames": 1,
        "mode": "RGB",
    }
    assert result.thumbnail is not None
    assert result.thumbnail.mime_type in {"image/jpeg", "image/png"}
    with Image.open(io.BytesIO(result.thumbnail.content)) as thumbnail:
        assert max(thumbnail.size) <= 128


def test_pdf_preview_has_page_count_text_and_first_page_thumbnail(
    tmp_path: Path,
    processor: PreviewProcessor,
) -> None:
    path = tmp_path / "document.pdf"
    document = pymupdf.open()
    page = document.new_page(width=300, height=200)
    page.insert_text((20, 40), "bounded artifact preview")
    document.new_page(width=300, height=200)
    document.save(path)
    document.close()

    result = processor.process(path, "pdf")

    assert result.status == "available"
    assert result.structural_summary["page_count"] == 2
    assert "bounded artifact preview" in result.structural_summary["text"]
    assert result.thumbnail is not None
    assert result.thumbnail.mime_type == "image/png"


def test_csv_json_and_text_structural_summaries_are_bounded(
    tmp_path: Path,
    processor: PreviewProcessor,
) -> None:
    csv_path = tmp_path / "data.csv"
    csv_path.write_text("name,value\nalpha,1\nbeta,2\ngamma,3\n", encoding="utf-8")
    csv_result = processor.process(csv_path, "csv")
    assert csv_result.structural_summary["columns"] == ["name", "value"]
    assert csv_result.structural_summary["row_count"] == 3
    assert len(csv_result.structural_summary["sample"]) == 2
    assert csv_result.structural_summary["column_stats"][1] == {
        "name": "value",
        "non_empty_count": 3,
        "numeric_count": 3,
        "numeric_min": 1.0,
        "numeric_max": 3.0,
    }

    json_path = tmp_path / "data.json"
    json_path.write_text(
        json.dumps({"records": [{"name": "alpha"}], "count": 1}),
        encoding="utf-8",
    )
    json_result = processor.process(json_path, "json")
    assert json_result.structural_summary["top_level_type"] == "object"
    assert json_result.structural_summary["top_level_keys"] == ["records", "count"]

    text_path = tmp_path / "notes.txt"
    text_path.write_text("first\nsecond\n", encoding="utf-8")
    text_result = processor.process(text_path, "text")
    assert text_result.structural_summary["encoding"] == "utf-8"
    assert text_result.structural_summary["line_count"] == 3


def test_binary_text_and_over_limit_inputs_are_unsupported(
    tmp_path: Path,
    processor: PreviewProcessor,
) -> None:
    binary = tmp_path / "binary"
    binary.write_bytes(b"\x00\x01\x02")
    result = processor.process(binary, "text")
    assert result.status == "unsupported"
    assert result.structural_summary["reason"] == "binary_content"

    oversized = tmp_path / "oversized"
    oversized.write_bytes(b"x" * (processor.config.max_input_bytes + 1))
    result = processor.process(oversized, "text")
    assert result.status == "unsupported"
    assert result.structural_summary["reason"] == "input_too_large"


@pytest.mark.parametrize(
    ("preview_type", "contents", "error_code"),
    [
        ("image", b"not an image", "ARTIFACT_PREVIEW_INVALID_IMAGE"),
        ("pdf", b"not a pdf", "ARTIFACT_PREVIEW_INVALID_PDF"),
        ("json", b"{broken", "ARTIFACT_PREVIEW_INVALID_JSON"),
        ("json", b'{"value":NaN}', "ARTIFACT_PREVIEW_INVALID_JSON"),
        ("csv", b"\xff", "ARTIFACT_PREVIEW_INVALID_ENCODING"),
    ],
)
def test_corrupt_supported_formats_fail_safely(
    tmp_path: Path,
    processor: PreviewProcessor,
    preview_type: str,
    contents: bytes,
    error_code: str,
) -> None:
    path = tmp_path / preview_type
    path.write_bytes(contents)
    with pytest.raises(PreviewFailure) as caught:
        processor.process(path, preview_type)
    assert caught.value.code == error_code

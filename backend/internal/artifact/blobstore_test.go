package artifact

import (
	"errors"
	"testing"
)

func TestCalculateMultipartPlanUsesConfiguredSize(t *testing.T) {
	plan, err := CalculateMultipartPlan(
		40*1024*1024,
		8*1024*1024,
		1024*1024*1024,
	)
	if err != nil {
		t.Fatalf("calculate plan: %v", err)
	}
	if plan.PartBytes != 8*1024*1024 || plan.PartCount != 5 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if size, err := plan.PartSize(5); err != nil || size != 8*1024*1024 {
		t.Fatalf("unexpected final part: size=%d err=%v", size, err)
	}
}

func TestCalculateMultipartPlanGrowsAtMiBBoundary(t *testing.T) {
	size := int64(MultipartMaxParts)*(5*1024*1024) + 1
	plan, err := CalculateMultipartPlan(size, MultipartMinPartBytes, MultipartMaxObjectBytes)
	if err != nil {
		t.Fatalf("calculate plan: %v", err)
	}
	if plan.PartBytes != 6*1024*1024 {
		t.Fatalf("expected 6 MiB parts, got %d", plan.PartBytes)
	}
	if plan.PartCount > MultipartMaxParts {
		t.Fatalf("part limit exceeded: %d", plan.PartCount)
	}
}

func TestCalculateMultipartPlanRoundsConfiguredSizeAtMiBBoundary(t *testing.T) {
	plan, err := CalculateMultipartPlan(
		20*1024*1024,
		MultipartMinPartBytes+1,
		1024*1024*1024,
	)
	if err != nil {
		t.Fatalf("calculate plan: %v", err)
	}
	if plan.PartBytes != 6*1024*1024 {
		t.Fatalf("expected 6 MiB rounded part size, got %d", plan.PartBytes)
	}
}

func TestCalculateMultipartPlanRejectsLimits(t *testing.T) {
	for _, test := range []struct {
		name      string
		size      int64
		part      int64
		maxUpload int64
	}{
		{name: "negative size", size: -1, part: MultipartMinPartBytes, maxUpload: 1},
		{name: "small part", size: 1, part: MultipartMinPartBytes - 1, maxUpload: 1},
		{name: "configured max", size: 2, part: MultipartMinPartBytes, maxUpload: 1},
		{
			name:      "provider max",
			size:      MultipartMaxObjectBytes + 1,
			part:      MultipartMinPartBytes,
			maxUpload: MultipartMaxObjectBytes,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CalculateMultipartPlan(
				test.size,
				test.part,
				test.maxUpload,
			); err == nil {
				t.Fatal("expected invalid plan")
			}
		})
	}
}

func TestMultipartPlanReportsFinalPartAndBounds(t *testing.T) {
	plan, err := CalculateMultipartPlan(
		12*1024*1024+123,
		5*1024*1024,
		1024*1024*1024,
	)
	if err != nil {
		t.Fatal(err)
	}
	if size, err := plan.PartSize(3); err != nil || size != 2*1024*1024+123 {
		t.Fatalf("unexpected final part: size=%d err=%v", size, err)
	}
	if _, err := plan.PartSize(0); !errors.Is(err, ErrInvalidPart) {
		t.Fatalf("expected invalid part, got %v", err)
	}
}

func TestValidateObjectKey(t *testing.T) {
	if err := ValidateObjectKey("projects/project/blobs/sha256/ab/hash"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	for _, key := range []string{"", "/absolute", "../escape", "a/../escape", `a\b`, "."} {
		if err := ValidateObjectKey(key); !errors.Is(err, ErrInvalidObjectKey) {
			t.Fatalf("expected invalid key %q, got %v", key, err)
		}
	}
}

package pagination

import "testing"

func TestCursorRoundTrip(t *testing.T) {
	input := Cursor{ID: "item-1", SortValue: "2026-07-28T00:00:00Z"}
	encoded, err := Encode(input)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if decoded.ID != input.ID || decoded.Version != 1 {
		t.Fatalf("unexpected decoded cursor: %#v", decoded)
	}
}

func TestRequestLimitValidation(t *testing.T) {
	normalized, err := (Request{}).Normalize()
	if err != nil || normalized.Limit != DefaultLimit {
		t.Fatalf("unexpected default request: %#v, %v", normalized, err)
	}
	if _, err := (Request{Limit: MaxLimit + 1}).Normalize(); err == nil {
		t.Fatal("expected oversized limit to fail")
	}
}

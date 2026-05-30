package integlife

import (
	"strings"
	"testing"
	"time"
)

func TestAITaskEventPayloadHashCanonicalStable(t *testing.T) {
	occurredAt := time.Date(2026, 5, 30, 2, 0, 0, 123456789, time.FixedZone("SGT", 8*60*60))
	base := AITaskEventRecord{
		RunUUID:    "run-1",
		TodoUUID:   "todo-1",
		EventType:  "artifact",
		Severity:   "info",
		Title:      "Created patch",
		Content:    "Updated CLI",
		OccurredAt: occurredAt,
	}

	first := base
	first.MetadataJSON = `{"z":true,"a":["one",{"b":null}]}`
	firstHash, firstMetadata, err := ComputeAITaskEventPayloadHash(first)
	if err != nil {
		t.Fatalf("ComputeAITaskEventPayloadHash(first) error = %v", err)
	}

	second := base
	second.MetadataJSON = `{"a":["one",{"b":null}],"z":true}`
	secondHash, secondMetadata, err := ComputeAITaskEventPayloadHash(second)
	if err != nil {
		t.Fatalf("ComputeAITaskEventPayloadHash(second) error = %v", err)
	}

	if firstHash != secondHash {
		t.Fatalf("hashes differ for reordered metadata: %s vs %s", firstHash, secondHash)
	}
	if firstMetadata != secondMetadata || firstMetadata != `{"a":["one",{"b":null}],"z":true}` {
		t.Fatalf("canonical metadata = %q / %q", firstMetadata, secondMetadata)
	}

	withNumber := base
	withNumber.MetadataJSON = `{"confidence":0.82}`
	_, _, err = ComputeAITaskEventPayloadHash(withNumber)
	if err == nil || !strings.Contains(err.Error(), "numbers are not allowed") {
		t.Fatalf("number metadata error = %v, want numbers rejected", err)
	}
}

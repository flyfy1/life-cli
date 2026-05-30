package integlife

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func CanonicalMetadataJSON(metadata string) (string, error) {
	if strings.TrimSpace(metadata) == "" {
		metadata = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(metadata))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("parse metadata_json: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", fmt.Errorf("metadata_json contains extra values")
	}
	if err := rejectJSONNumbers(value); err != nil {
		return "", err
	}
	out, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("canonicalize metadata_json: %w", err)
	}
	return string(out), nil
}

func ComputeAITaskEventPayloadHash(event AITaskEventRecord) (string, string, error) {
	metadata, err := CanonicalMetadataJSON(event.MetadataJSON)
	if err != nil {
		return "", "", err
	}
	occurredAt := event.OccurredAt.UTC().Format(time.RFC3339Nano)
	input := []string{
		event.RunUUID,
		event.TodoUUID,
		event.EventType,
		event.Severity,
		event.Title,
		event.Content,
		metadata,
		occurredAt,
	}
	sum := sha256.Sum256([]byte(strings.Join(input, "\x1f")))
	return hex.EncodeToString(sum[:]), metadata, nil
}

func rejectJSONNumbers(value any) error {
	switch typed := value.(type) {
	case json.Number:
		return fmt.Errorf("metadata_json numbers are not allowed in payload_hash v1")
	case []any:
		for _, item := range typed {
			if err := rejectJSONNumbers(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range typed {
			if err := rejectJSONNumbers(item); err != nil {
				return err
			}
		}
	}
	return nil
}

package sigil

import (
	"fmt"
	"testing"
)

func TestRecordedGenerationIDsAreBounded(t *testing.T) {
	client := &Client{config: Config{GenerationExport: GenerationExportConfig{QueueSize: 1}}}
	limit := client.recordedGenerationLimit()
	for i := 0; i < limit+10; i++ {
		client.recordGenerationID(fmt.Sprintf("gen-%d", i))
	}

	if len(client.generationIDs) != limit {
		t.Fatalf("expected %d recorded generation ids, got %d", limit, len(client.generationIDs))
	}
	if client.hasRecordedGenerationID("gen-0") {
		t.Fatal("expected oldest generation id to be evicted")
	}
	if !client.hasRecordedGenerationID(fmt.Sprintf("gen-%d", limit+9)) {
		t.Fatal("expected newest generation id to remain recorded")
	}
}

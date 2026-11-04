package replay

import (
	"strings"
	"testing"
)

func TestNewBoundedDatasetReaderRejectsInvalidLimits(t *testing.T) {
	if got := NewBoundedDatasetReader(strings.NewReader("{}\n"), 0, 1); got != nil {
		t.Fatal("expected nil reader for zero byte limit")
	}
	if got := NewBoundedDatasetReader(strings.NewReader("{}\n"), 1, 0); got != nil {
		t.Fatal("expected nil reader for zero episode limit")
	}
	if got := NewBoundedDatasetReader(nil, 1024, 1); got != nil {
		t.Fatal("expected nil reader for nil input")
	}
}

func TestDatasetReaderRejectsOversizedEpisode(t *testing.T) {
	reader := NewBoundedDatasetReader(strings.NewReader("{\"steps\":[]}\n"), 4, 10)
	if reader == nil {
		t.Fatal("expected reader")
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("expected oversized episode error")
	}
}

func TestDatasetReaderHonorsCustomEpisodeLimit(t *testing.T) {
	input := "{}\n{}\n"
	reader := NewBoundedDatasetReader(strings.NewReader(input), 1024, 1)
	if reader == nil {
		t.Fatal("expected reader")
	}
	if _, err := reader.Next(); err != nil {
		t.Fatalf("first episode: %v", err)
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("expected episode limit error")
	}
}

func TestDatasetReaderRejectsMalformedJSON(t *testing.T) {
	reader := NewDatasetReader(strings.NewReader("not-json\n"))
	if _, err := reader.Next(); err == nil {
		t.Fatal("expected malformed JSON error")
	}
}

func TestDatasetReaderAcceptsExactRecordLimit(t *testing.T) {
	input := "{}\n"
	reader := NewBoundedDatasetReader(strings.NewReader(input), 2, 1)
	if reader == nil {
		t.Fatal("expected reader")
	}
	if _, err := reader.Next(); err != nil {
		t.Fatalf("expected exact-size record to be accepted: %v", err)
	}
}

func TestDatasetReaderRejectsRecordBeyondScannerLimit(t *testing.T) {
	input := strings.Repeat("x", 1024) + "\n"
	reader := NewBoundedDatasetReader(strings.NewReader(input), 1023, 1)
	if reader == nil {
		t.Fatal("expected reader")
	}
	if _, err := reader.Next(); err == nil {
		t.Fatal("expected oversized record error")
	}
}

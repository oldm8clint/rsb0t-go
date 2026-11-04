package publicingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// IngestJSONL reads one Lesson per JSONL line. Invalid lessons are rejected
// individually so a single malformed contributor record cannot poison a batch.
func IngestJSONL(r io.Reader, fn func(Lesson) error) error {
	s := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	s.Buffer(buf, 1024*1024)
	line := 0
	for s.Scan() {
		line++
		text := strings.TrimSpace(s.Text())
		if text == "" {
			continue
		}
		var lesson Lesson
		if err := json.Unmarshal([]byte(text), &lesson); err != nil {
			return fmt.Errorf("lesson line %d: decode: %w", line, err)
		}
		if err := Validate(lesson); err != nil {
			return fmt.Errorf("lesson line %d: validate: %w", line, err)
		}
		if err := fn(lesson); err != nil {
			return fmt.Errorf("lesson line %d: store: %w", line, err)
		}
	}
	if err := s.Err(); err != nil {
		return fmt.Errorf("read lessons: %w", err)
	}
	return nil
}

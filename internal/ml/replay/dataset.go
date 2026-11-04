package replay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	// DefaultMaxEpisodeBytes bounds one JSONL record so malformed datasets cannot
	// force an unbounded allocation in the offline evaluator.
	DefaultMaxEpisodeBytes int64 = 4 << 20
	DefaultMaxEpisodes           = 100000
)

// DatasetReader streams episodes without loading a complete dataset into RAM.
type DatasetReader struct {
	reader      *bufio.Reader
	maxBytes    int64
	maxEpisodes int
	episodes    int
	line        []byte
}

func NewDatasetReader(r io.Reader) *DatasetReader {
	return NewBoundedDatasetReader(r, DefaultMaxEpisodeBytes, DefaultMaxEpisodes)
}

// NewBoundedDatasetReader applies explicit record and dataset-count limits.
// Limits must be positive.
func NewBoundedDatasetReader(r io.Reader, maxEpisodeBytes int64, maxEpisodes int) *DatasetReader {
	if maxEpisodeBytes <= 0 || maxEpisodes <= 0 || r == nil {
		return nil
	}
	if maxEpisodeBytes > int64(int(^uint(0)>>1))-1 {
		return nil
	}
	return &DatasetReader{
		reader:      bufio.NewReaderSize(r, 64<<10),
		maxBytes:    maxEpisodeBytes,
		maxEpisodes: maxEpisodes,
	}
}

func (r *DatasetReader) Next() (*Episode, error) {
	if r == nil || r.reader == nil {
		return nil, ErrDatasetReaderNil
	}
	if r.episodes >= r.maxEpisodes {
		return nil, ErrDatasetLimitExceeded
	}

	var err error
	r.line, err = readBoundedLine(r.reader, r.line[:0], r.maxBytes)
	if err != nil {
		return nil, err
	}

	var e Episode
	if err := json.Unmarshal(r.line, &e); err != nil {
		return nil, fmt.Errorf("decode episode: %w", err)
	}
	r.episodes++
	return &e, nil
}

// readBoundedLine returns one JSONL record without ever retaining more than
// maxBytes of record data plus a small amount of delimiter state. The caller's
// buffer is reused across records to avoid repeated large allocations.
func readBoundedLine(r *bufio.Reader, out []byte, maxBytes int64) ([]byte, error) {
	for {
		part, err := r.ReadSlice('\n')
		if len(part) > 0 {
			content := part
			if err == nil {
				content = part[:len(part)-1]
			}
			if int64(len(out))+int64(len(content)) > maxBytes {
				return nil, fmt.Errorf("%w: exceeds %d bytes", ErrEpisodeTooLarge, maxBytes)
			}
			out = append(out, content...)
		}
		if err == nil {
			return bytes.TrimSuffix(out, []byte{'\r'}), nil
		}
		if err != bufio.ErrBufferFull {
			if err == io.EOF && len(out) > 0 {
				return bytes.TrimSuffix(out, []byte{'\r'}), nil
			}
			if err == io.EOF {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("read episode: %w", err)
		}
	}
}

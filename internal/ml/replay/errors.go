package replay

import "errors"

var (
	ErrDatasetReaderNil     = errors.New("nil dataset reader")
	ErrDatasetLimitExceeded = errors.New("dataset episode limit exceeded")
	ErrEpisodeTooLarge      = errors.New("episode record too large")
)

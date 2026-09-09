package domain

import "errors"

var (
	ErrInvalidQuery       = errors.New("invalid query")
	ErrInvalidDocument    = errors.New("invalid document")
	ErrInvalidChunk       = errors.New("invalid chunk")
	ErrMetadataValidation = errors.New("metadata validation failed")
)

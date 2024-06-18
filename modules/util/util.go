package util

import (
	"sync"

	"github.com/google/uuid"
)

// CloseOnce :
type CloseOnce struct {
	C     chan struct{}
	Once  sync.Once
	Close func()
}

// NewID : new uuid
func NewID() (string, error) {
	rid, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return rid.String(), nil
}

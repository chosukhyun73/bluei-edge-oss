package common

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropy     = ulid.Monotonic(rand.Reader, 0)
	entropyLock sync.Mutex
)

func newULID() string {
	entropyLock.Lock()
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
	entropyLock.Unlock()
	return id.String()
}

func NewID(prefix string) string { return prefix + "_" + newULID() }

func NewEventID() string   { return NewID("evt") }
func NewReadingID() string { return NewID("reading") }
func NewCommandID() string { return NewID("cmd") }
func NewAlertID() string   { return NewID("alert") }
func NewBatchID() string   { return NewID("batch") }
func NewStatusID() string  { return NewID("status") }
func NewCorrID() string    { return NewID("corr") }

package domain

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync/atomic"
	"time"
)

// Identifier prefixes. Every aggregate in Orxest uses a prefixed, opaque,
// time-sortable identifier. Prefixes make it possible to recognise the kind of
// an identifier in logs, URLs and persisted event payloads without a lookup.
const (
	IDPrefixProject        = "prj"
	IDPrefixIssue          = "iss"
	IDPrefixTask           = "tsk"
	IDPrefixRole           = "rol"
	IDPrefixAgent          = "agt"
	IDPrefixProjectAgent   = "pag"
	IDPrefixWorkflow       = "wfl"
	IDPrefixWorkflowStep   = "wfs"
	IDPrefixExecution      = "exe"
	IDPrefixExecutionEvent = "exv"
	IDPrefixEvent          = "evt"
	IDPrefixRepository     = "rep"
)

// idSequence makes identifiers strictly increasing within one process, even
// when several are created inside the same millisecond. Combined with the
// timestamp it means "sort by id" equals "sort by creation order", which the
// scheduler relies on as its deterministic tie-breaker.
var idSequence atomic.Uint64

// NewID returns a unique, lexicographically sortable identifier of the form
//
//	<prefix>_<unix milliseconds (8 bytes)><sequence (4 bytes)><random (2 bytes)>
//
// all hex encoded. The random tail keeps identifiers from different processes
// distinct; the sequence makes them monotonic inside one process.
func NewID(prefix string) string {
	var buf [14]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(time.Now().UnixMilli()))
	binary.BigEndian.PutUint32(buf[8:12], uint32(idSequence.Add(1)))
	if _, err := rand.Read(buf[12:]); err != nil {
		// crypto/rand never fails on supported platforms; if it does the
		// process cannot safely continue issuing identifiers.
		panic(fmt.Sprintf("domain: cannot read entropy: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(buf[:])
}

// NewEventID is a convenience wrapper used by the event bus.
func NewEventID() string { return NewID(IDPrefixEvent) }

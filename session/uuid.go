package session

import (
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

var createSessionID = func() string { return UUIDv7() }

func CreateSessionID() string { return createSessionID() }

func CreateSessionId() string { return CreateSessionID() }

var uuidv7State struct {
	sync.Mutex
	lastMilli uint64
	lastTail  [10]byte
}

func CreateTimestamp() string {
	return strings.TrimSuffix(time.Now().UTC().Format(time.RFC3339Nano), "Z") + "+00:00"
}

func UUIDv7() string {
	var bytes [16]byte
	now := uint64(time.Now().UnixMilli())
	uuidv7State.Lock()
	defer uuidv7State.Unlock()
	if now > uuidv7State.lastMilli {
		_, _ = rand.Read(uuidv7State.lastTail[:])
		uuidv7State.lastMilli = now
	} else {
		now = uuidv7State.lastMilli
		incrementUUIDv7Tail(&uuidv7State.lastTail)
	}
	copy(bytes[6:], uuidv7State.lastTail[:])
	bytes[0] = byte(now >> 40)
	bytes[1] = byte(now >> 32)
	bytes[2] = byte(now >> 24)
	bytes[3] = byte(now >> 16)
	bytes[4] = byte(now >> 8)
	bytes[5] = byte(now)
	bytes[6] = (bytes[6] & 0x0f) | 0x70
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

func Uuidv7() string { return UUIDv7() }

func incrementUUIDv7Tail(tail *[10]byte) {
	for i := len(tail) - 1; i >= 0; i-- {
		tail[i]++
		if tail[i] != 0 {
			return
		}
	}
}

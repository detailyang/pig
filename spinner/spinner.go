package spinner

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var Frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const FrameInterval = 80 * time.Millisecond

type Sink interface {
	Write([]byte)
}

type SpinnerSink = Sink

type stderrSink struct{}

func (stderrSink) Write(bytes []byte) { _, _ = os.Stderr.Write(bytes) }

type BufferSink struct {
	mu  sync.Mutex
	Buf []byte
}

func NewBufferSink() *BufferSink { return &BufferSink{} }

func (sink *BufferSink) Write(bytes []byte) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.Buf = append(sink.Buf, bytes...)
}

func (sink *BufferSink) Bytes() []byte {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return append([]byte(nil), sink.Buf...)
}

func (sink *BufferSink) String() string { return string(sink.Bytes()) }

func (sink *BufferSink) AsString() string { return sink.String() }

type Handle struct {
	stop       *atomic.Bool
	sink       Sink
	enabled    bool
	stopOnDrop bool
}

type Snapshot struct {
	Enabled      bool
	Stopped      bool
	BytesWritten int
}

type SpinnerHandle = Handle

func Start(label string) *SpinnerHandle {
	enabled := false
	if stat, err := os.Stderr.Stat(); err == nil {
		enabled = stat.Mode()&os.ModeCharDevice != 0
	}
	return StartWith(label, stderrSink{}, enabled)
}

func StartWith(label string, sink Sink, enabled bool) *Handle {
	stop := &atomic.Bool{}
	handle := &Handle{stop: stop, sink: sink, enabled: enabled, stopOnDrop: true}
	if !enabled || sink == nil {
		return handle
	}
	drawFrame(sink, 0, label)
	go func() {
		index := 1
		for {
			time.Sleep(FrameInterval)
			if stop.Load() {
				return
			}
			drawFrame(sink, index, label)
			index++
		}
	}()
	return handle
}

func (handle *Handle) Clone() *Handle {
	if handle == nil {
		return nil
	}
	return &Handle{stop: handle.stop, sink: handle.sink, enabled: handle.enabled, stopOnDrop: false}
}

func (handle *Handle) Stop() {
	if handle == nil || handle.stop == nil {
		return
	}
	if handle.stop.Swap(true) {
		return
	}
	if handle.enabled && handle.sink != nil {
		handle.sink.Write([]byte("\r\x1b[2K"))
	}
}

func (handle *Handle) StopSync() { handle.Stop() }

func (handle *Handle) Snapshot() Snapshot {
	if handle == nil {
		return Snapshot{Stopped: true}
	}
	snapshot := Snapshot{Enabled: handle.enabled}
	if handle.stop == nil || handle.stop.Load() {
		snapshot.Stopped = true
	}
	if buffer, ok := handle.sink.(*BufferSink); ok {
		snapshot.BytesWritten = len(buffer.Bytes())
	}
	return snapshot
}

func drawFrame(sink Sink, index int, label string) {
	frame := Frames[index%len(Frames)]
	sink.Write([]byte(fmt.Sprintf("\r\x1b[2K%s %s", frame, label)))
}

package spinner

import (
	"strings"
	"testing"
	"time"
)

func TestFirstFrameRendersSynchronously(t *testing.T) {
	sink := NewBufferSink()
	var sinkAlias SpinnerSink = sink
	var handle *SpinnerHandle = StartWith("thinking", sinkAlias, true)
	defer handle.StopSync()
	body := sink.AsString()
	if !strings.Contains(body, "⠋") || !strings.Contains(body, "thinking") {
		t.Fatalf("first frame missing: %q", body)
	}
}

func TestBufferSinkExposesBufferLikeUpstream(t *testing.T) {
	sink := NewBufferSink()
	sink.Write([]byte("hello"))
	if string(sink.Buf) != "hello" {
		t.Fatalf("buffer mismatch: %q", sink.Buf)
	}
}

func TestSpinnerAnimatesAndStopClearsImmediately(t *testing.T) {
	sink := NewBufferSink()
	handle := StartWith("thinking", sink, true)
	time.Sleep(220 * time.Millisecond)
	before := sink.String()
	handle.Stop()
	after := sink.String()
	if !strings.HasSuffix(after, "\r\x1b[2K") {
		t.Fatalf("stop should clear line, before=%q after=%q", before, after)
	}
	distinct := 0
	for _, frame := range Frames {
		if strings.Contains(after, frame) {
			distinct++
		}
	}
	if distinct < 2 {
		t.Fatalf("expected at least two frames, got %d in %q", distinct, after)
	}
}

func TestStopIsIdempotentAndCloneDropDoesNotStopOwner(t *testing.T) {
	sink := NewBufferSink()
	handle := StartWith("thinking", sink, true)
	_ = handle.Clone()
	before := len(sink.Bytes())
	time.Sleep(120 * time.Millisecond)
	if after := len(sink.Bytes()); after <= before {
		t.Fatalf("dropping clone should not stop owner before=%d after=%d", before, after)
	}
	handle.Stop()
	afterFirst := len(sink.Bytes())
	handle.Stop()
	afterSecond := len(sink.Bytes())
	if afterFirst != afterSecond {
		t.Fatalf("second stop wrote bytes: %d -> %d", afterFirst, afterSecond)
	}
}

func TestDisabledSpinnerIsSilent(t *testing.T) {
	sink := NewBufferSink()
	handle := StartWith("thinking", sink, false)
	time.Sleep(120 * time.Millisecond)
	handle.Stop()
	if got := sink.String(); got != "" {
		t.Fatalf("disabled spinner wrote %q", got)
	}
}

func TestSpinnerSnapshotReportsState(t *testing.T) {
	sink := NewBufferSink()
	handle := StartWith("thinking", sink, true)
	snapshot := handle.Snapshot()
	if !snapshot.Enabled || snapshot.Stopped || snapshot.BytesWritten == 0 {
		t.Fatalf("running snapshot mismatch: %#v", snapshot)
	}
	handle.StopSync()
	snapshot = handle.Snapshot()
	if !snapshot.Stopped || snapshot.BytesWritten != len(sink.Bytes()) {
		t.Fatalf("stopped snapshot mismatch: %#v len=%d", snapshot, len(sink.Bytes()))
	}
}

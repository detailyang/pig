package ai

import (
	"errors"
	"testing"
)

func TestAIErrorVariantConstructorsMatchUpstreamNames(t *testing.T) {
	cause := errors.New("dial failed")
	bedrock := BedrockErrorNetwork("dial failed")
	if bedrock.Kind != BedrockErrorExchange || bedrock.Error() != "network error: dial failed" {
		t.Fatalf("bedrock network mismatch: %#v", bedrock)
	}
	vertex := VertexErrorNetwork("dial failed")
	if vertex.Kind != VertexErrorExchange || vertex.Error() != "network error: dial failed" {
		t.Fatalf("vertex network mismatch: %#v", vertex)
	}
	if AdcErrorIo("missing").Error() != "io: missing" || AdcErrorExchange("bad").Error() != "token exchange: bad" {
		t.Fatalf("adc error constructors mismatch")
	}
	retry := RetrySendErrorReqwest(cause)
	if retry.Kind != RetrySendErrorRequest || !errors.Is(retry, cause) {
		t.Fatalf("retry reqwest mismatch: %#v", retry)
	}
	abort := AbortErrorOrReqwestReqwest(cause)
	if abort.Kind != AbortErrorOrReqwestRequest || !errors.Is(abort, cause) {
		t.Fatalf("abort reqwest mismatch: %#v", abort)
	}
}

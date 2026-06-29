package ai

import "testing"

func TestSanitizeSurrogatesStringIsIdentityLikeUpstream(t *testing.T) {
	input := "hello 世界 😀"
	if got := SanitizeSurrogates(input); got != input {
		t.Fatalf("valid strings should be unchanged like upstream: %q", got)
	}
}

func TestSanitizeSurrogatesUTF16DropsUnpairedSurrogatesLikeUpstream(t *testing.T) {
	input := []uint16{'a', 0xD800, 0xD83D, 0xDE00, 0xDC00, 'b'}
	if got := SanitizeSurrogatesUTF16(input); got != "a😀b" {
		t.Fatalf("unpaired surrogates should be dropped while pairs are preserved, got %q", got)
	}
	if got := SanitizeSurrogatesU16(input); got != "a😀b" {
		t.Fatalf("upstream u16 helper should match UTF16 helper, got %q", got)
	}
}

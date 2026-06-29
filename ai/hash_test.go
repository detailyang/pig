package ai

import "testing"

func TestShortHashMatchesUpstreamUTF16Algorithm(t *testing.T) {
	cases := map[string]string{
		"":           "k4n83c7h0j2b",
		"hello":      "1h6qa0qrowduu",
		"世界":         "14dxwau1tjtqpb",
		"😀":          "13wj7r7usi372",
		"hello 世界 😀": "1poj35g10biv07",
	}
	for input, want := range cases {
		if got := ShortHash(input); got != want {
			t.Fatalf("ShortHash(%q)=%q want %q", input, got, want)
		}
	}
}

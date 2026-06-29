package ai

import (
	"strconv"
	"unicode/utf16"
)

func ShortHash(value string) string {
	hash1 := uint32(0xdeadbeef)
	hash2 := uint32(0x41c6ce57)
	for _, unit := range utf16.Encode([]rune(value)) {
		codeUnit := uint32(unit)
		hash1 = (hash1 ^ codeUnit) * 2_654_435_761
		hash2 = (hash2 ^ codeUnit) * 1_597_334_677
	}
	hash1 = ((hash1 ^ (hash1 >> 16)) * 2_246_822_507) ^ ((hash2 ^ (hash2 >> 13)) * 3_266_489_909)
	hash2 = ((hash2 ^ (hash2 >> 16)) * 2_246_822_507) ^ ((hash1 ^ (hash1 >> 13)) * 3_266_489_909)
	return toBase36(hash2) + toBase36(hash1)
}

func toBase36(value uint32) string {
	return strconv.FormatUint(uint64(value), 36)
}

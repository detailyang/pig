package ai

import "unicode/utf16"

func SanitizeSurrogates(text string) string {
	return text
}

func SanitizeSurrogatesUTF16(buffer []uint16) string {
	out := make([]uint16, 0, len(buffer))
	for index := 0; index < len(buffer); {
		unit := buffer[index]
		if 0xD800 <= unit && unit <= 0xDBFF {
			if index+1 < len(buffer) && 0xDC00 <= buffer[index+1] && buffer[index+1] <= 0xDFFF {
				out = append(out, unit, buffer[index+1])
				index += 2
				continue
			}
			index++
			continue
		}
		if 0xDC00 <= unit && unit <= 0xDFFF {
			index++
			continue
		}
		out = append(out, unit)
		index++
	}
	return string(utf16.Decode(out))
}

func SanitizeSurrogatesU16(buffer []uint16) string {
	return SanitizeSurrogatesUTF16(buffer)
}

package markdown

import "strings"

const Reset = "\x1b[0m"
const Bold = "\x1b[1m"
const Italic = "\x1b[3m"
const Dim = "\x1b[2m"
const Code = "\x1b[2;36m"
const Heading = "\x1b[1;34m"

func RenderLine(line string) string {
	if level, ok := headingLevel(line); ok {
		body := line[level+1:]
		return Heading + strings.Repeat("#", level) + body + Reset
	}
	return renderInline(line)
}

type Renderer struct {
	inFence bool
}

func NewRenderer() *Renderer {
	return &Renderer{}
}

func DefaultRenderer() *Renderer {
	return NewRenderer()
}

func Default() *Renderer {
	return NewRenderer()
}

func (renderer *Renderer) RenderLine(line string) string {
	if strings.HasPrefix(line, "```") {
		renderer.inFence = !renderer.inFence
		return Dim + line + Reset
	}
	if renderer.inFence {
		return Code + line + Reset
	}
	return RenderLine(line)
}

func headingLevel(line string) (int, bool) {
	level := 0
	for _, char := range line {
		if char == '#' {
			level++
			continue
		}
		if char == ' ' && level > 0 && level <= 6 {
			return level, true
		}
		return 0, false
	}
	return 0, false
}

func renderInline(line string) string {
	var out strings.Builder
	out.Grow(len(line))
	for index := 0; index < len(line); {
		if line[index] == '`' {
			if end := FindByte([]byte(line), index+1, '`'); end >= 0 {
				out.WriteString(Code)
				out.WriteString(line[index+1 : end])
				out.WriteString(Reset)
				index = end + 1
				continue
			}
		}
		if index+1 < len(line) && line[index] == '*' && line[index+1] == '*' {
			if end := findDoubleStar(line, index+2); end >= 0 {
				out.WriteString(Bold)
				out.WriteString(line[index+2 : end])
				out.WriteString(Reset)
				index = end + 2
				continue
			}
		}
		if line[index] == '*' && index+1 < len(line) && line[index+1] != ' ' && line[index+1] != '*' {
			if end := findSingleStar(line, index+1); end >= 0 {
				out.WriteString(Italic)
				out.WriteString(line[index+1 : end])
				out.WriteString(Reset)
				index = end + 1
				continue
			}
		}
		out.WriteByte(line[index])
		index++
	}
	return out.String()
}

func FindByte(buffer []byte, from int, value byte) int {
	if from < 0 || from >= len(buffer) {
		return -1
	}
	for index := from; index < len(buffer); index++ {
		if buffer[index] == value {
			return index
		}
	}
	return -1
}

func findDoubleStar(line string, from int) int {
	for index := from; index+1 < len(line); index++ {
		if line[index] == '*' && line[index+1] == '*' {
			return index
		}
	}
	return -1
}

func findSingleStar(line string, from int) int {
	for index := from; index < len(line); index++ {
		if line[index] == '*' {
			if index+1 < len(line) && line[index+1] == '*' {
				index++
				continue
			}
			return index
		}
	}
	return -1
}

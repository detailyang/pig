package ai

func TypeboxString(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func TypeboxBoolean(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func TypeboxNumber(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func TypeboxObject(properties any, required []string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func String(description string) map[string]any {
	return TypeboxString(description)
}

func Boolean(description string) map[string]any {
	return TypeboxBoolean(description)
}

func Number(description string) map[string]any {
	return TypeboxNumber(description)
}

func Object(properties any, required []string) map[string]any {
	return TypeboxObject(properties, required)
}

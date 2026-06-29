package triggers

func ExtractDynamicRuleIDs(text string) []string {
	var ids []string
	bytes := []byte(text)
	for index := 0; index+4 <= len(bytes); {
		if string(bytes[index:index+4]) != "dyn-" {
			index++
			continue
		}
		start := index
		index += 4
		for index < len(bytes) && isASCIIHex(bytes[index]) {
			index++
		}
		if index-start == 36 {
			id := string(bytes[start:index])
			if !containsDynamicID(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func HandleDynamicTriggerCompletion(registry *DynamicRegistry, summary string) ([]DynamicRule, error) {
	if registry == nil {
		return nil, nil
	}
	return registry.MarkRulesFired(ExtractDynamicRuleIDs(summary))
}

func isASCIIHex(value byte) bool {
	return ('0' <= value && value <= '9') || ('a' <= value && value <= 'f') || ('A' <= value && value <= 'F')
}

func containsDynamicID(ids []string, id string) bool {
	for _, existing := range ids {
		if existing == id {
			return true
		}
	}
	return false
}

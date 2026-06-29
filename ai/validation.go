package ai

type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

func Validate(input any, schema any) ValidationResult {
	_ = input
	_ = schema
	return ValidationResult{Valid: true}
}

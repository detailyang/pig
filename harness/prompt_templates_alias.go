package harness

import "github.com/detailyang/pig/templates"

type LoadTemplatesOutput = templates.LoadTemplatesOutput
type PromptTemplateRegistry = templates.PromptTemplateRegistry

func NewPromptTemplateRegistry(templateList []PromptTemplate) PromptTemplateRegistry {
	return templates.NewPromptTemplateRegistry(templateList)
}

func LoadTemplates(dirs []string) LoadTemplatesOutput {
	return templates.LoadTemplates(dirs)
}

func InterpolatePromptTemplate(template PromptTemplate, vars map[string]any) string {
	return templates.Interpolate(template, vars)
}

package model

import "github.com/detailyang/pig/ai"

type Model = ai.Model
type Provider = ai.Provider

func AutoDetectModel(overrideProvider string, overrideModel string) (Model, error) {
	return ai.AutoDetectModel(overrideProvider, overrideModel)
}

func CredentialLessDefault() Model {
	return ai.CredentialLessDefault()
}

func FirstModelForProvider(provider string) (Model, bool) {
	return ai.FirstModelForProvider(provider)
}

func ExplicitModelNotFoundMessage(provider string, id string) string {
	return ai.ExplicitModelNotFoundMessage(provider, id)
}

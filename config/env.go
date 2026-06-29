package config

import "github.com/detailyang/pig/ai"

func EnvVarNames(provider string) []string {
	return ai.EnvVarNames(provider)
}

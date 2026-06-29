package proxy

import (
	"net/http"

	"github.com/detailyang/pig/ai"
)

func ProxyFromEnv() (string, bool) {
	return ai.ProxyFromEnv()
}

func BuildClient(timeoutMS int64) (*http.Client, error) {
	return ai.BuildClient(timeoutMS)
}

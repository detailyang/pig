package node

import "github.com/detailyang/pig/tools"

type NativeEnv = tools.NativeEnv

func NewNativeEnv(cwd string) NativeEnv {
	return tools.NewNativeEnv(cwd)
}

func CurrentNativeEnv() (NativeEnv, error) {
	return tools.CurrentNativeEnv()
}

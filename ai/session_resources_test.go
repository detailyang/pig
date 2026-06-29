package ai

import "testing"

func TestCleanupSessionResourcesIsIdempotent(t *testing.T) {
	CleanupSessionResources()
	CleanupSessionResources()
}

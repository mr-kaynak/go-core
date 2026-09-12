package repository

import "testing"

func requireRepositorySetup(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("repository setup failed: %v", err)
	}
}

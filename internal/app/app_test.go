package app

import "testing"

func TestRun(t *testing.T) {
	if err := Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

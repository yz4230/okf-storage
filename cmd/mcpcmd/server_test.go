package mcpcmd

import "testing"

func TestInstructions(t *testing.T) {
	cs, _ := connect(t)
	if got := cs.InitializeResult().Instructions; got != instructions || got == "" {
		t.Errorf("Instructions = %q, want %q", got, instructions)
	}
}

package gologix

import (
	"math"
	"testing"
)

func TestNextListInstanceStartKeepsLastReturnedInstance(t *testing.T) {
	if got := nextListInstanceStart(41); got != 41 {
		t.Fatalf("expected 0x55 continuation to reuse the last returned instance, got %d", got)
	}
}

func TestNextListInstanceStartKeepsMaxUint32(t *testing.T) {
	if got := nextListInstanceStart(math.MaxUint32); got != math.MaxUint32 {
		t.Fatalf("expected max uint32 to stay unchanged, got %d", got)
	}
}

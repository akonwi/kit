package droids

import (
	"math"
	"testing"
)

func TestMergeUsageRejectsOverflowWithoutPartialResult(t *testing.T) {
	maximum := int(^uint(0) >> 1)
	if _, err := mergeUsage(Usage{Input: maximum, Output: 7}, Usage{Input: 1, Output: 1}); err == nil {
		t.Fatal("mergeUsage accepted integer overflow")
	}
	if _, err := mergeUsage(Usage{Cost: UsageCost{Total: math.MaxFloat64}}, Usage{Cost: UsageCost{Total: math.MaxFloat64}}); err == nil {
		t.Fatal("mergeUsage accepted cost overflow")
	}
}

func TestValidateUsageRejectsNonFiniteCost(t *testing.T) {
	for _, cost := range []float64{math.Inf(1), math.NaN()} {
		if err := validateUsage(Usage{Cost: UsageCost{Total: cost}}); err == nil {
			t.Fatalf("validateUsage accepted %v", cost)
		}
	}
	if err := validateUsage(Usage{}); err != nil {
		t.Fatalf("validateUsage zero = %v", err)
	}
}

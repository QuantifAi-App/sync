package parser

import (
	"math"
	"testing"
)

// costTolerance is the maximum acceptable difference between the Go and Python
// cost calculations, per the spec requirement of < 0.0001 USD.
const costTolerance = 0.0001

// TestPricingAllModels verifies that GetPricing returns the correct rates for
// each of the 5 model variants in the pricing table.
func TestPricingAllModels(t *testing.T) {
	tests := []struct {
		model       string
		wantInput   float64
		wantOutput  float64
		wantCRead   float64
		wantCCreate float64
	}{
		{"claude-opus-4-6", 15.0, 75.0, 1.875, 18.75},
		{"claude-opus-4-5-20251101", 15.0, 75.0, 1.875, 18.75},
		{"claude-sonnet-4-6", 3.0, 15.0, 0.375, 3.75},
		{"claude-sonnet-4-5-20241022", 3.0, 15.0, 0.375, 3.75},
		{"claude-haiku-4-5-20251001", 0.80, 4.0, 0.08, 1.0},
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			p := GetPricing(tc.model)
			if p.Input != tc.wantInput {
				t.Errorf("Input: got %v, want %v", p.Input, tc.wantInput)
			}
			if p.Output != tc.wantOutput {
				t.Errorf("Output: got %v, want %v", p.Output, tc.wantOutput)
			}
			if p.CacheRead != tc.wantCRead {
				t.Errorf("CacheRead: got %v, want %v", p.CacheRead, tc.wantCRead)
			}
			if p.CacheCreate != tc.wantCCreate {
				t.Errorf("CacheCreate: got %v, want %v", p.CacheCreate, tc.wantCCreate)
			}
		})
	}
}

// TestPricingSubstringMatchAndFallback verifies that substring matching works
// for model names that contain a known key, and that completely unknown models
// fall back to Opus pricing.
func TestPricingSubstringMatchAndFallback(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		wantInput float64
	}{
		// Substring match: a model string containing a known key
		{"sonnet substring", "some-prefix-claude-sonnet-4-6-suffix", 3.0},
		{"haiku substring", "prefix-claude-haiku-4-5-20251001-beta", 0.80},
		// Unknown model falls back to Opus
		{"unknown model", "claude-unknown-99", 15.0},
		{"empty string", "", 15.0},
		{"random string", "gpt-4-turbo", 15.0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := GetPricing(tc.model)
			if p.Input != tc.wantInput {
				t.Errorf("Input for %q: got %v, want %v", tc.model, p.Input, tc.wantInput)
			}
		})
	}
}

// TestCostCalculationAccuracy verifies that CalculateCost produces results
// matching the Python formula within floating-point tolerance.  The reference
// values are computed by hand using the Python formula from src/ingestion.py.
func TestCostCalculationAccuracy(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		input        int
		output       int
		cacheRead    int
		cacheCreate  int
		expectedCost float64
	}{
		{
			name:         "opus typical record",
			model:        "claude-opus-4-6",
			input:        3,
			output:       11,
			cacheRead:    18719,
			cacheCreate:  2176,
			// Python: 3*15/1e6 + 11*75/1e6 + 18719*1.875/1e6 + 2176*18.75/1e6
			//       = 0.000045 + 0.000825 + 0.035098125 + 0.040800
			//       = 0.076768125
			expectedCost: 0.076768125,
		},
		{
			name:         "sonnet moderate",
			model:        "claude-sonnet-4-6",
			input:        1000,
			output:       500,
			cacheRead:    5000,
			cacheCreate:  0,
			// Python: 1000*3/1e6 + 500*15/1e6 + 5000*0.375/1e6 + 0
			//       = 0.003 + 0.0075 + 0.001875 + 0
			//       = 0.012375
			expectedCost: 0.012375,
		},
		{
			name:         "haiku small",
			model:        "claude-haiku-4-5-20251001",
			input:        100,
			output:       50,
			cacheRead:    0,
			cacheCreate:  0,
			// Python: 100*0.80/1e6 + 50*4.0/1e6 + 0 + 0
			//       = 0.00008 + 0.0002 = 0.00028
			expectedCost: 0.00028,
		},
		{
			name:         "zero tokens",
			model:        "claude-opus-4-6",
			input:        0,
			output:       0,
			cacheRead:    0,
			cacheCreate:  0,
			expectedCost: 0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := GetPricing(tc.model)
			got := CalculateCost(p, tc.input, tc.output, tc.cacheRead, tc.cacheCreate)
			diff := math.Abs(got - tc.expectedCost)
			if diff > costTolerance {
				t.Errorf("cost for %q: got %.10f, want %.10f (diff %.10f > tolerance %v)",
					tc.name, got, tc.expectedCost, diff, costTolerance)
			}
		})
	}
}

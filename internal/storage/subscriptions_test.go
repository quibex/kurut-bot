package storage

import "testing"

func TestNormalizePhoneVariants(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "with plus returns both variants",
			input:    "+996552295229",
			expected: []string{"+996552295229", "996552295229"},
		},
		{
			name:     "without plus returns both variants",
			input:    "996552295229",
			expected: []string{"+996552295229", "996552295229"},
		},
		{
			name:     "russian with plus",
			input:    "+79017250082",
			expected: []string{"+79017250082", "79017250082"},
		},
		{
			name:     "russian without plus",
			input:    "79017250082",
			expected: []string{"+79017250082", "79017250082"},
		},
		{
			name:     "with spaces trimmed",
			input:    " +996552295229 ",
			expected: []string{"+996552295229", "996552295229"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePhoneVariants(tt.input)

			if len(result) != len(tt.expected) {
				t.Fatalf("normalizePhoneVariants(%q) returned %d variants, want %d", tt.input, len(result), len(tt.expected))
			}

			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("normalizePhoneVariants(%q)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestNormalizePhoneVariantsContainsBoth(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldMatch string
	}{
		{
			name:        "search with plus finds imported without plus",
			input:       "+996552295229",
			shouldMatch: "996552295229",
		},
		{
			name:        "search without plus finds created with plus",
			input:       "996552295229",
			shouldMatch: "+996552295229",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			variants := normalizePhoneVariants(tt.input)

			found := false
			for _, v := range variants {
				if v == tt.shouldMatch {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("normalizePhoneVariants(%q) should contain %q, got %v", tt.input, tt.shouldMatch, variants)
			}
		})
	}
}

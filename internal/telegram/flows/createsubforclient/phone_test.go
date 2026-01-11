package createsubforclient

import "testing"

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already normalized with plus",
			input:    "+79017250082",
			expected: "+79017250082",
		},
		{
			name:     "with spaces",
			input:    "+7 901 725 00 82",
			expected: "+79017250082",
		},
		{
			name:     "with dashes",
			input:    "+7-901-725-00-82",
			expected: "+79017250082",
		},
		{
			name:     "with spaces and dashes",
			input:    "+7 901 725-00-82",
			expected: "+79017250082",
		},
		{
			name:     "with parentheses",
			input:    "+7 (901) 725-00-82",
			expected: "+79017250082",
		},
		{
			name:     "without plus",
			input:    "79017250082",
			expected: "79017250082",
		},
		{
			name:     "without plus with spaces",
			input:    "7 901 725 00 82",
			expected: "79017250082",
		},
		{
			name:     "kyrgyzstan format",
			input:    "+996555123456",
			expected: "+996555123456",
		},
		{
			name:     "kyrgyzstan with spaces",
			input:    "+996 555 123 456",
			expected: "+996555123456",
		},
		{
			name:     "plus in the middle ignored",
			input:    "7901+7250082",
			expected: "79017250082",
		},
		{
			name:     "only digits",
			input:    "89017250082",
			expected: "89017250082",
		},
		{
			name:     "with dots",
			input:    "+7.901.725.00.82",
			expected: "+79017250082",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePhone(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePhone(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsValidPhoneNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "valid russian with plus",
			input:    "+79017250082",
			expected: true,
		},
		{
			name:     "valid russian without plus",
			input:    "79017250082",
			expected: true,
		},
		{
			name:     "valid kyrgyzstan",
			input:    "+996555123456",
			expected: true,
		},
		{
			name:     "valid 10 digits",
			input:    "9017250082",
			expected: true,
		},
		{
			name:     "valid 15 digits",
			input:    "+123456789012345",
			expected: true,
		},
		{
			name:     "too short 9 digits",
			input:    "901725008",
			expected: false,
		},
		{
			name:     "too long 16 digits",
			input:    "1234567890123456",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "only plus",
			input:    "+",
			expected: false,
		},
		{
			name:     "with letters",
			input:    "+7901abc0082",
			expected: false,
		},
		{
			name:     "not normalized - should fail",
			input:    "+7 901 725 00 82",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidPhoneNumber(tt.input)
			if result != tt.expected {
				t.Errorf("IsValidPhoneNumber(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantNorm      string
		wantValid     bool
	}{
		{
			name:      "russian with spaces and dashes",
			input:     "+7 901 725-00-82",
			wantNorm:  "+79017250082",
			wantValid: true,
		},
		{
			name:      "russian with parentheses",
			input:     "+7 (901) 725-00-82",
			wantNorm:  "+79017250082",
			wantValid: true,
		},
		{
			name:      "kyrgyzstan clean",
			input:     "+996555123456",
			wantNorm:  "+996555123456",
			wantValid: true,
		},
		{
			name:      "russian 8 format",
			input:     "8 901 725-00-82",
			wantNorm:  "89017250082",
			wantValid: true,
		},
		{
			name:      "too short after normalize",
			input:     "+7 901",
			wantNorm:  "+7901",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := NormalizePhone(tt.input)
			if normalized != tt.wantNorm {
				t.Errorf("NormalizePhone(%q) = %q, want %q", tt.input, normalized, tt.wantNorm)
			}

			valid := IsValidPhoneNumber(normalized)
			if valid != tt.wantValid {
				t.Errorf("IsValidPhoneNumber(%q) = %v, want %v", normalized, valid, tt.wantValid)
			}
		})
	}
}

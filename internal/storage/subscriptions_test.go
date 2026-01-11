package storage

import "testing"

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"+996 552 295 229", "996552295229"},
		{"996552295229", "996552295229"},
		{"+996552295229", "996552295229"},
		{"+7-926-330-85-06", "79263308506"},
		{"+7 (926) 330-85-06", "79263308506"},
		{"8 800 555 35 35", "88005553535"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizePhone(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizePhone(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

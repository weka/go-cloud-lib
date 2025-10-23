package bash_functions

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPrefixToNetmask(t *testing.T) {
	tests := []struct {
		prefix   string
		expected string
	}{
		{"8", "255.0.0.0"},
		{"16", "255.255.0.0"},
		{"20", "255.255.240.0"},
		{"24", "255.255.255.0"},
		{"28", "255.255.255.240"},
		{"32", "255.255.255.255"},
		{"0", "0.0.0.0"},
	}

	// Get the bash function
	bashScript := PrefixToNetmask()

	for _, tt := range tests {
		t.Run("prefix_"+tt.prefix, func(t *testing.T) {
			// Create a bash script that sources the function and calls it
			script := bashScript + "\nprefix_to_netmask " + tt.prefix

			cmd := exec.Command("bash", "-c", script)
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Failed to execute bash function: %v", err)
			}

			result := strings.TrimSpace(string(output))
			if result != tt.expected {
				t.Errorf("prefix_to_netmask(%s) = %s; want %s", tt.prefix, result, tt.expected)
			}
		})
	}
}

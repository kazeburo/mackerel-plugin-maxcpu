package statworker

import (
	"os"
	"strings"
	"testing"
)

func TestParseCPUstat(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
		wantErr  bool
	}{
		{"100", 1.0, false},
		{"0", 0.0, false},
		{"200", 2.0, false},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		got, err := parseCPUstat([]byte(tt.input))
		if (err != nil) != tt.wantErr {
			t.Errorf("parseCPUstat(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.expected {
			t.Errorf("parseCPUstat(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestGetCPUStat(t *testing.T) {
	// Simulate a /proc/stat line
	procStat := "cpu  168487 7399 36999 7766545 3915 0 13480 0 0 0\n"
	f := tmpFileWithContent(t, procStat)
	defer os.Remove(f.Name())

	stat, err := getCPUStat(f)
	if err != nil {
		t.Fatalf("getCPUStat() error = %v", err)
	}
	if stat.User == 0 || stat.Idle == 0 {
		t.Errorf("Expected non-zero User and Idle, got User=%v Idle=%v", stat.User, stat.Idle)
	}
}

func TestGetCPUStat_NoCPULine(t *testing.T) {
	procStat := "intr 12345\nctxt 67890\n"
	f := tmpFileWithContent(t, procStat)
	defer os.Remove(f.Name())

	_, err := getCPUStat(f)
	if err == nil || !strings.Contains(err.Error(), "no cpu stats found") {
		t.Errorf("Expected error for missing cpu line, got %v", err)
	}
}

func TestGetCPUStat_ShortCPULine(t *testing.T) {
	procStat := "cpu \n"
	f := tmpFileWithContent(t, procStat)
	defer os.Remove(f.Name())

	_, err := getCPUStat(f)
	if err == nil || !strings.Contains(err.Error(), "no cpu stats found") {
		t.Errorf("Expected error for short cpu line, got %v", err)
	}
}

// Helper to create a temp file with content and return *os.File
func tmpFileWithContent(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp("", "statworker_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("failed to seek temp file: %v", err)
	}
	return f
}

func TestGetCPUStat_HandlesExtraSpaces(t *testing.T) {
	// Extra spaces between fields
	procStat := "cpu    100   200  300  400  500  600  700  800  900  1000\n"
	f := tmpFileWithContent(t, procStat)
	defer os.Remove(f.Name())

	stat, err := getCPUStat(f)
	if err != nil {
		t.Fatalf("getCPUStat() error = %v", err)
	}
	if stat.User != 1.0 || stat.Nice != 2.0 || stat.System != 3.0 || stat.Idle != 4.0 {
		t.Errorf("Unexpected parsed values: %+v", stat)
	}
}

func TestGetCPUStat_HandlesMissingFields(t *testing.T) {
	// Only 4 fields
	procStat := "cpu  100 200 300 400\n"
	f := tmpFileWithContent(t, procStat)
	defer os.Remove(f.Name())

	stat, err := getCPUStat(f)
	if err != nil {
		t.Fatalf("getCPUStat() error = %v", err)
	}
	if stat.User != 1.0 || stat.Nice != 2.0 || stat.System != 3.0 || stat.Idle != 4.0 {
		t.Errorf("Unexpected parsed values: %+v", stat)
	}
}

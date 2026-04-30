package logger

import (
	"testing"

	"github.com/sirupsen/logrus"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected logrus.Level
	}{
		{"debug", logrus.DebugLevel},
		{"DEBUG", logrus.DebugLevel},
		{"info", logrus.InfoLevel},
		{"INFO", logrus.InfoLevel},
		{"warn", logrus.WarnLevel},
		{"warning", logrus.WarnLevel},
		{"error", logrus.ErrorLevel},
		{"invalid", logrus.InfoLevel},
		{"", logrus.InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"juan.perez@gmail.com", "ju***@gmail.com"},
		{"ab@gmail.com", "***@gmail.com"},
		{"a@gmail.com", "***@gmail.com"},
		{"", ""},
		{"invalid", "***"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := MaskEmail(tt.input)
			if result != tt.expected {
				t.Errorf("MaskEmail(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sk_test_abc123xyz789", "sk_t***z789"},
		{"short", "***"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := MaskAPIKey(tt.input)
			if result != tt.expected {
				t.Errorf("MaskAPIKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMaskPhone(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"+56912345678", "+5691234****"},
		{"1234", "***"},
		{"123", "***"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := MaskPhone(tt.input)
			if result != tt.expected {
				t.Errorf("MaskPhone(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFieldsFromKVs(t *testing.T) {
	fields := fieldsFromKVs("key1", "val1", "key2", 42)
	if fields["key1"] != "val1" {
		t.Errorf("fields[key1] = %v, want val1", fields["key1"])
	}
	if fields["key2"] != 42 {
		t.Errorf("fields[key2] = %v, want 42", fields["key2"])
	}

	fields = fieldsFromKVs()
	if len(fields) != 0 {
		t.Errorf("expected empty fields, got %d", len(fields))
	}
}

func TestSugaredLogger_Infow(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	entry := logger.WithField("base", "value")
	sl := &SugaredLogger{
		entry:  entry,
		logger: logger,
		config: Config{Service: "test", Version: "1.0", Environment: "dev"},
	}

	// Should not panic
	sl.Infow("test_message", "key", "val")
	sl.Warnw("test_warn", "key", "val")
	sl.Errorw("test_error", "key", "val")
	sl.Debugw("test_debug", "key", "val")
}

func TestSugaredLogger_With(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	entry := logger.WithField("base", "value")
	sl := &SugaredLogger{
		entry:  entry,
		logger: logger,
		config: Config{Service: "test", Version: "1.0", Environment: "dev"},
	}

	withLogger := sl.With("extra", "field")
	if withLogger == sl {
		t.Error("With should return a new instance")
	}

	withLogger.Infow("test_with", "key", "val")
}

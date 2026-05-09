package middleware

import (
	"testing"
)

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		host     string
		expected bool
	}{
		{"", true},
		{"localhost", true},
		{"localhost:3000", true},
		{"127.0.0.1", true},
		{"127.0.0.1:8080", true},
		{"[::1]", true},
		{"[::1]:8080", true},
		{"dev.reservaloya.cl", false},
		{"api.dev.reservaloya.cl", false},
		{"reservaloya.cl", false},
		{"192.168.1.1", false},
		{"example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result := isLocalhost(tt.host)
			if result != tt.expected {
				t.Errorf("isLocalhost(%q) = %v, want %v", tt.host, result, tt.expected)
			}
		})
	}
}

func TestCookieDomain(t *testing.T) {
	tests := []struct {
		host     string
		expected string
	}{
		// localhost → ""
		{"", ""},
		{"localhost", ""},
		{"localhost:3000", ""},
		{"127.0.0.1", ""},
		{"127.0.0.1:8080", ""},
		{"[::1]", ""},
		// subdomain of reservaloya.cl → .reservaloya.cl (two or more dots in hostname)
		{"dev.reservaloya.cl", ".reservaloya.cl"},
		{"api.dev.reservaloya.cl", ".dev.reservaloya.cl"},
		{"staging.reservaloya.cl", ".reservaloya.cl"},
		{"www.example.com", ".example.com"},
		// single domain (0 or 1 dots) → ""
		{"reservaloya.cl", ""},
		{"example.com", ""},
		// with port
		{"dev.reservaloya.cl:443", ".reservaloya.cl"},
		{"api.dev.reservaloya.cl:8080", ".dev.reservaloya.cl"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			result := cookieDomain(tt.host)
			if result != tt.expected {
				t.Errorf("cookieDomain(%q) = %q, want %q", tt.host, result, tt.expected)
			}
		})
	}
}

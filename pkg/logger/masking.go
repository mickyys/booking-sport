package logger

import (
	"strings"
)

func MaskEmail(email string) string {
	if email == "" {
		return ""
	}

	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "***"
	}

	username := parts[0]
	domain := parts[1]

	if len(username) <= 2 {
		username = "***"
	} else {
		username = username[:2] + "***"
	}

	return username + "@" + domain
}

func MaskAPIKey(key string) string {
	if key == "" {
		return ""
	}

	if len(key) <= 8 {
		return "***"
	}

	return key[:4] + "***" + key[len(key)-4:]
}

func MaskPhone(phone string) string {
	if phone == "" {
		return ""
	}

	if len(phone) <= 4 {
		return "***"
	}

	return phone[:len(phone)-4] + "****"
}

func MaskString(s string, visibleChars int) string {
	if s == "" {
		return ""
	}

	if len(s) <= visibleChars {
		return "***"
	}

	return s[:visibleChars] + "***"
}

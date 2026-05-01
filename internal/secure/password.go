package secure

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (reasonable defaults; can be made configurable)
const (
	argonTime    = 2
	argonMemory  = 64 * 1024 // 64MB
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// HashPassword returns an encoded hash string: base64(salt):base64(hash)
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(hash), nil
}

// VerifyPassword checks a password against the stored encoded value.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := split(encoded)
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid encoded hash")
	}
	salt, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return false, err
	}
	expected, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return false, err
	}
	calc := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	if len(calc) != len(expected) {
		return false, nil
	}
	// constant-time compare
	var diff byte
	for i := range calc {
		diff |= calc[i] ^ expected[i]
	}
	return diff == 0, nil
}

func split(s string) []string {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

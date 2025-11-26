package common

import (
	"crypto/hmac"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

func GenerateHMACWithKey(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func GenerateHMAC(data string) string {
	h := hmac.New(sha256.New, []byte(CryptoSecret))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func Password2Hash(password string) (string, error) {
	passwordBytes := []byte(password)
	hashedPassword, err := bcrypt.GenerateFromPassword(passwordBytes, bcrypt.DefaultCost)
	return string(hashedPassword), err
}

func ValidatePasswordAndHash(password string, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateSecurePassword generates a strong random password with specified length.
// The password contains uppercase letters, lowercase letters, digits, and special characters.
// This is used for external authentication users who don't need to know their password.
func GenerateSecurePassword(length int) (string, error) {
	if length < 8 {
		length = 32 // Default to 32 characters for security
	}

	// Character sets
	const (
		lowercase = "abcdefghijklmnopqrstuvwxyz"
		uppercase = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		digits    = "0123456789"
		special   = "!@#$%^&*"
		allChars  = lowercase + uppercase + digits + special
	)

	password := make([]byte, length)
	allCharsLen := big.NewInt(int64(len(allChars)))

	// Ensure password contains at least one character from each category
	// First 4 positions: one from each category
	categories := []string{lowercase, uppercase, digits, special}
	for i := 0; i < 4 && i < length; i++ {
		category := categories[i]
		categoryLen := big.NewInt(int64(len(category)))
		n, err := crand.Int(crand.Reader, categoryLen)
		if err != nil {
			return "", err
		}
		password[i] = category[n.Int64()]
	}

	// Fill remaining positions with random characters
	for i := 4; i < length; i++ {
		n, err := crand.Int(crand.Reader, allCharsLen)
		if err != nil {
			return "", err
		}
		password[i] = allChars[n.Int64()]
	}

	// Shuffle the password to avoid predictable pattern
	for i := length - 1; i > 0; i-- {
		n, err := crand.Int(crand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		j := n.Int64()
		password[i], password[j] = password[j], password[i]
	}

	return string(password), nil
}

package auth

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Password rules (ADR-0031 D8). The password is an RCE credential: whoever
// logs in gets a terminal and every agent, so the floor is 12 characters and
// there is no default. bcrypt only reads the first 72 bytes, so a longer
// password would be silently truncated; it is rejected instead.
const (
	MinPasswordLength = 12
	maxPasswordBytes  = 72
	// bcryptCost is deliberately above the library default (10): a hash is
	// computed once per set-password / daemon start and checked once per
	// accepted login attempt, never per request.
	bcryptCost = 12
)

// ErrPasswordTooShort and ErrPasswordTooLong describe rejected passwords.
var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d bytes (bcrypt ignores the rest)", maxPasswordBytes)
)

// ValidatePassword enforces the length rules.
func ValidatePassword(pw string) error {
	if utf8.RuneCountInString(pw) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(pw) > maxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns the bcrypt hash of a password that passed
// ValidatePassword.
func HashPassword(pw string) (string, error) {
	if err := ValidatePassword(pw); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// CheckPassword reports whether pw matches the bcrypt hash. bcrypt compares in
// constant time with respect to the password; a malformed hash or an
// over-long password simply does not match.
func CheckPassword(hash, pw string) bool {
	if hash == "" || len(pw) > maxPasswordBytes {
		return false
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
	return err == nil
}

// ValidateHash checks that s looks like a bcrypt hash (a config typo would
// otherwise lock every login out silently).
func ValidateHash(s string) error {
	if _, err := bcrypt.Cost([]byte(s)); err != nil {
		if errors.Is(err, bcrypt.ErrHashTooShort) {
			return errors.New("server.passwordHash is not a bcrypt hash; run `smind set-password`")
		}
		return fmt.Errorf("server.passwordHash is not a valid bcrypt hash (%v); run `smind set-password`", err)
	}
	return nil
}

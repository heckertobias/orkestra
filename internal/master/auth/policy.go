package auth

import (
	"fmt"
	"unicode"

	"github.com/heckertobias/orkestra/internal/master/store"
)

// ValidatePassword checks pw against the configured policy.
// Returns a human-readable error if any rule is violated, nil otherwise.
// Pass a zero-value store.PasswordPolicy to skip all checks.
func ValidatePassword(p store.PasswordPolicy, pw string) error {
	if p.MinLength > 0 && len([]rune(pw)) < int(p.MinLength) {
		return fmt.Errorf("password must be at least %d characters", p.MinLength)
	}

	var special, digits, upper, lower int
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r) && unicode.IsUpper(r):
			upper++
		case unicode.IsLetter(r) && unicode.IsLower(r):
			lower++
		case unicode.IsDigit(r):
			digits++
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			special++
		}
	}

	if err := checkRange("special character", special, int(p.SpecialMin), int(p.SpecialMax)); err != nil {
		return err
	}
	if err := checkRange("digit", digits, int(p.DigitMin), int(p.DigitMax)); err != nil {
		return err
	}
	if err := checkRange("uppercase letter", upper, int(p.UpperMin), int(p.UpperMax)); err != nil {
		return err
	}
	if err := checkRange("lowercase letter", lower, int(p.LowerMin), int(p.LowerMax)); err != nil {
		return err
	}
	return nil
}

// checkRange verifies that count is within [min, max].
// min=0 means no minimum; max=0 means unlimited.
func checkRange(kind string, count, min, max int) error {
	if min > 0 && count < min {
		if min == 1 {
			return fmt.Errorf("password must contain at least 1 %s", kind)
		}
		return fmt.Errorf("password must contain at least %d %ss", min, kind)
	}
	if max > 0 && count > max {
		if max == 0 {
			return fmt.Errorf("password must not contain any %ss", kind)
		}
		return fmt.Errorf("password must contain at most %d %ss", max, kind)
	}
	return nil
}

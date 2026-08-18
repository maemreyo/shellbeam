package verification

import (
	"fmt"
	"regexp"
)

var activationIDPattern = regexp.MustCompile(`^act_[A-Za-z0-9_-]{1,120}$`)
var waiverIDPattern = regexp.MustCompile(`^wv_[A-Za-z0-9_-]{1,121}$`)

func ValidateActivationID(v string) error {
	if !activationIDPattern.MatchString(v) {
		return fmt.Errorf("invalid activation id")
	}
	return nil
}
func ValidateWaiverID(v string) error {
	if !waiverIDPattern.MatchString(v) {
		return fmt.Errorf("invalid waiver id")
	}
	return nil
}

package rsmq

import (
	"errors"
	"regexp"
)

// Validation errors
var (
	ErrInvalidQname   = errors.New("queue name is in wrong pattern")
	ErrInvalidVt      = errors.New("visibility timeout is out of range [0, 9999999]")
	ErrInvalidDelay   = errors.New("delay is out of range [0, 9999999]")
	ErrInvalidMaxsize = errors.New("max size is out of range [1024, 65536] and not -1")
	ErrInvalidID      = errors.New("id is in wrong pattern")
)

var (
	regexpQname = regexp.MustCompile(`^([a-zA-Z\d_-]){1,160}$`)
	regexpID    = regexp.MustCompile(`^([a-zA-Z\d:]){32}$`)
)

func validateQname(val string) error {
	if !regexpQname.MatchString(val) {
		return ErrInvalidQname
	}
	return nil
}

func validateVt(val uint) error {
	if val > 9999999 {
		return ErrInvalidVt
	}
	return nil
}

func validateDelay(val uint) error {
	if val > 9999999 {
		return ErrInvalidDelay
	}
	return nil
}

func validateMaxsize(val int) error {
	if val != -1 && (val < 1024 || val > 65536) {
		return ErrInvalidMaxsize
	}
	return nil
}

func validateID(val string) error {
	if !regexpID.MatchString(val) {
		return ErrInvalidID
	}
	return nil
}

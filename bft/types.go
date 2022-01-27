package bft

import (
	"errors"
)

var errConflictWithCommitted = errors.New("block conflict with committeed")

func IsConflictWithCommitted(err error) bool {
	return err == errConflictWithCommitted
}

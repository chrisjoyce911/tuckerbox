package fleet

import (
	"errors"
	"fmt"
)

// OwnershipError definitively fences a released or replaced holder.
type OwnershipError struct{ Machine string }

func (e *OwnershipError) Error() string {
	if e.Machine == "" {
		return "claim released"
	}
	return "claim taken by " + e.Machine
}
func OwnershipLost(err error) bool { var e *OwnershipError; return errors.As(err, &e) }

// TransportError allows callers to retry a failed git operation without
// mistaking lack of connectivity for proof that ownership changed.
type TransportError struct{ Err error }

func (e *TransportError) Error() string { return e.Err.Error() }
func (e *TransportError) Unwrap() error { return e.Err }
func Temporary(err error) bool          { var e *TransportError; return errors.As(err, &e) }
func transport(format string, args ...any) error {
	return &TransportError{Err: fmt.Errorf(format, args...)}
}

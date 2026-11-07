package domain

import "errors"

// ErrInvalidRequest reports a request that violates the domain contract.
var ErrInvalidRequest = errors.New("invalid request")

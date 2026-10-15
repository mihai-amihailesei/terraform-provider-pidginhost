// Copyright (c) The terraform-provider-pidginhost authors
// SPDX-License-Identifier: MPL-2.0

package apiclient

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrWaitFailed marks a terminal failure reported by the API while waiting
// (for example a server whose status became "failed").
var ErrWaitFailed = errors.New("operation failed")

// WaitFor calls check every interval until it reports done, returns an
// error, or ctx expires. check returns a short description of the current
// state, used in the timeout message.
func WaitFor(ctx context.Context, interval time.Duration, what string, check func(ctx context.Context) (done bool, state string, err error)) error {
	var last string
	for {
		done, state, err := check(ctx)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		last = state

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if last != "" {
				return fmt.Errorf("timed out waiting for %s (last state: %s)", what, last)
			}
			return fmt.Errorf("timed out waiting for %s", what)
		case <-timer.C:
		}
	}
}

// Server and bucket statuses (ResourceStatusEnum).
const (
	StatusPending      = "pending"
	StatusActive       = "active"
	StatusProvisioning = "provisioning"
	StatusSuspending   = "suspending"
	StatusSuspended    = "suspended"
	StatusResuming     = "resuming"
	StatusCancelling   = "cancelling"
	StatusCancelled    = "cancelled"
	StatusTerminating  = "terminating"
	StatusTerminated   = "terminated"
	StatusFailed       = "failed"
	StatusUpgrading    = "upgrading"
)

// IsGoneStatus reports whether a ResourceStatusEnum value means the resource
// no longer exists for practical purposes.
func IsGoneStatus(s string) bool {
	return s == StatusCancelled || s == StatusTerminated
}

// IsTerminalFailure reports whether waiting for "active" can never succeed.
func IsTerminalFailure(s string) bool {
	return s == StatusFailed || s == StatusCancelling || s == StatusCancelled ||
		s == StatusTerminating || s == StatusTerminated || s == StatusSuspended
}

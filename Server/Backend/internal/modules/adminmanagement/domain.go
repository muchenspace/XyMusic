package adminmanagement

import "errors"

// ErrLastActiveAdministrator is returned when an update would remove the
// administrator access of the last active administrator.
var ErrLastActiveAdministrator = errors.New("the last active administrator cannot be removed")

// RemovesAdministratorAccess reports whether the requested update removes the
// user's administrator access.
func RemovesAdministratorAccess(
	currentRole UserRole,
	currentStatus UserStatus,
	nextRole *UserRole,
	nextStatus *UserStatus,
) bool {
	if currentRole != RoleAdmin || currentStatus != StatusActive {
		return false
	}
	if nextRole != nil && *nextRole == RoleUser {
		return true
	}
	return nextStatus != nil && (*nextStatus == StatusSuspended || *nextStatus == StatusDeleted)
}

// CanRemoveAdministrator validates that at least one other active
// administrator remains after administrator access is removed.
func CanRemoveAdministrator(otherActiveAdministrators int) error {
	if otherActiveAdministrators < 1 {
		return ErrLastActiveAdministrator
	}
	return nil
}

// RequiresSessionRevocation reports whether the requested update must revoke
// the user's active sessions.
func RequiresSessionRevocation(nextRole *UserRole, nextStatus *UserStatus) bool {
	if nextStatus != nil && (*nextStatus == StatusSuspended || *nextStatus == StatusDeleted) {
		return true
	}
	return nextRole != nil && *nextRole == RoleUser
}

// IsSelfSuspension reports whether an administrator is trying to suspend their
// own account.
func IsSelfSuspension(actorID, userID string, status UserStatus) bool {
	return actorID == userID && status == StatusSuspended
}

package admin

import (
	"encoding/json"
	"fmt"

	"github.com/matt-riley/flagz/internal/repository"
)

// buildAuditEntry constructs a repository.AuditLogEntry, marshalling the
// optional details value to JSON. Returns an error if details cannot be
// marshalled.
func buildAuditEntry(adminUserID, action, projectID, flagKey string, details any) (repository.AuditLogEntry, error) {
	entry := repository.AuditLogEntry{
		AdminUserID: adminUserID,
		Action:      action,
		ProjectID:   projectID,
		FlagKey:     flagKey,
	}

	if details != nil {
		raw, err := json.Marshal(details)
		if err != nil {
			return repository.AuditLogEntry{}, fmt.Errorf("marshal audit details: %w", err)
		}
		entry.Details = raw
	}

	return entry, nil
}

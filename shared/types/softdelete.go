package types

import "time"

// SoftDeletable provides deleted_at / deleted_by fields for soft-delete.
// Embed in any struct that needs soft-delete support.
type SoftDeletable struct {
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
	DeletedBy *string    `json:"deletedBy,omitempty"`
}

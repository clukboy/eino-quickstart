// Package usersource contains provider-neutral adapters for upstream user directories.
package usersource

import (
	"context"
)
type User struct {
	ExternalID string `json:"external_id"`
	Username   string `json:"username"`
	Nickname   string `json:"nickname"`
}

type Source interface {
	Fetch(context.Context) ([]*User, error)
	FetchByExternalID(ctx context.Context, externalID string) (*User, error)
}
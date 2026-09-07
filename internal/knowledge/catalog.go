package knowledge

import (
	"context"
	"eino-quickstart/ent"
	"eino-quickstart/ent/knowledgebase"
	"errors"
	"fmt"
	"strings"
)

func EnsureKnowledgeBase(ctx context.Context, client *ent.Client, name, ownerSubject, visibility string) (*ent.KnowledgeBase, error) {
	if client == nil {
		return nil, errors.New("knowledge base database client is required")
	}

	if ctx == nil {
		return nil, errors.New("knowledge base context is required")
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("knowledge base name is required")
	}

	ownerSubject = strings.TrimSpace(ownerSubject)
	if ownerSubject == "" {
		ownerSubject = "system"
	}

	visibility = strings.TrimSpace(visibility)

	if visibility == "" {
		visibility = "system"
	}

	if visibility != "system" && visibility != "private" {
		return nil, fmt.Errorf("knowledge base visibility %q is invalid", visibility)
	}

	existing, err := client.KnowledgeBase.Query().Where(knowledgebase.NameEQ(name)).Only(ctx)
	if err == nil {
		return existing, nil
	}
	if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("query knowledge base %q: %w", name, err)
	}

	created, err := client.KnowledgeBase.Create().
		SetName(name).
		SetOwnerSubject(ownerSubject).
		SetVisibility(knowledgebase.Visibility(visibility)).
		SetStatus(knowledgebase.DefaultStatus).
		Save(ctx)
	if err == nil {
		return created, nil
	}
	if ent.IsConstraintError(err) {
		existing, queryErr := client.KnowledgeBase.Query().Where(knowledgebase.NameEQ(name)).Only(ctx)
		if queryErr == nil {
			return existing, nil
		}
		return nil, fmt.Errorf("query knowledge base after create conflict: %w", queryErr)
	}
	return nil, fmt.Errorf("create knowledge base %q: %w", name, err)
}

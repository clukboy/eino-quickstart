// Package usersync owns the local side of external user-directory sync.
package usersync

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/platform/usersource"

	"golang.org/x/crypto/bcrypt"
)

type CreatedUser struct {
	ExternalID      string
	Username        string
	InitialPassword string
}

type UserError struct {
	ExternalID string
	Username   string
	Reason     string
}

type Report struct {
	DryRun       bool
	Total        int
	Created      int
	Updated      int
	Skipped      int
	Failed       int
	CreatedUsers []CreatedUser
	Errors       []UserError
}

type accountStore interface {
	ByExternalID(context.Context, string) (*account.Record, error)
	ByUsername(context.Context, string) (*account.Credentials, error)
	UpdateExternalUser(context.Context, uint64, string, string, string) error
	Create(context.Context, account.CreateParams) (*account.Record, error)
}

type Service struct {
	store      accountStore
	source     usersource.Source
	bcryptCost int
}

func NewService(store accountStore, source usersource.Source, bcryptCost int) (*Service, error) {
	if store == nil {
		return nil, errors.New("usersync: account store is required")
	}
	if source == nil {
		return nil, errors.New("usersync: source is required")
	}
	return &Service{store: store, source: source, bcryptCost: bcryptCost}, nil
}

func (s *Service) Sync(ctx context.Context, dryRun bool) (*Report, error) {
	upstream, err := s.source.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	report := &Report{DryRun: dryRun, Total: len(upstream)}
	seen := make(map[string]struct{}, len(upstream))
	for _, u := range upstream {
		if u.ExternalID == "" {
			report.Failed++
			report.Errors = append(report.Errors, UserError{ExternalID: u.ExternalID, Username: u.Username, Reason: "external_id is required"})
			continue
		}
		if _, ok := seen[u.ExternalID]; ok {
			report.Failed++
			report.Errors = append(report.Errors, UserError{ExternalID: u.ExternalID, Username: u.Username, Reason: "duplicate external_id in upstream response"})
			continue
		}
		seen[u.ExternalID] = struct{}{}

		existing, findErr := s.store.ByExternalID(ctx, u.ExternalID)
		if findErr != nil && !errors.Is(findErr, account.ErrNotFound) {
			return nil, fmt.Errorf("usersync: lookup %s: %w", u.ExternalID, findErr)
		}
		if existing != nil {
			if existing.Username == u.Username && existing.Nickname == u.Nickname {
				report.Skipped++
				continue
			}
			if dryRun {
				report.Updated++
				continue
			}
			if err := s.store.UpdateExternalUser(ctx, existing.ID, u.ExternalID, u.Username, u.Nickname); err != nil {
				report.Failed++
				report.Errors = append(report.Errors, UserError{ExternalID: u.ExternalID, Username: u.Username, Reason: publicReason(err)})
				continue
			}
			report.Updated++
			continue
		}

		// FetchByExternalID 只做「补全」：源能给就换成更完整的用户名/昵称，
		// 给不了（(nil, nil)）就继续用 Fetch 返回的那份。
		//
		// 不要写成 `u, err = ...`：那会在出错时把 u 覆盖成 nil，下面错误分支里的
		// u.ExternalID 直接解引用崩溃（线上表现是 400 + RecoverHandler 打栈，
		// 且整次同步在这里中断）。
		detail, err := s.source.FetchByExternalID(ctx, u.ExternalID)
		if err != nil {
			report.Failed++
			report.Errors = append(report.Errors, UserError{ExternalID: u.ExternalID, Username: u.Username, Reason: fmt.Sprintf("failed to fetch user by external ID: %v", err)})
			continue
		}
		if detail != nil {
			u = detail
		}
		record, err := s.store.ByUsername(ctx, u.Username)
		if err == nil {
			err = s.store.UpdateExternalUser(ctx, record.Record.ID, u.ExternalID, u.Username, u.Nickname)
			if err != nil {
				report.Failed++
				report.Errors = append(report.Errors, UserError{ExternalID: u.ExternalID, Username: u.Username, Reason: publicReason(err)})
				continue
			}
		} else if !errors.Is(err, account.ErrNotFound) {
			return nil, fmt.Errorf("usersync: username lookup %s: %w", u.Username, err)
		}
		if dryRun {
			report.Created++
			continue
		}
		password, err := randomPassword()
		if err != nil {
			return nil, fmt.Errorf("usersync: generate initial password: %w", err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), effectiveCost(s.bcryptCost))
		if err != nil {
			return nil, fmt.Errorf("usersync: hash initial password: %w", err)
		}
		externalID := u.ExternalID
		if _, err := s.store.Create(ctx, account.CreateParams{Username: u.Username, Nickname: u.Nickname, PasswordHash: string(hash), ExternalID: &externalID}); err != nil {
			report.Failed++
			report.Errors = append(report.Errors, UserError{ExternalID: u.ExternalID, Username: u.Username, Reason: publicReason(err)})
			continue
		}
		report.Created++
		report.CreatedUsers = append(report.CreatedUsers, CreatedUser{ExternalID: u.ExternalID, Username: u.Username, InitialPassword: password})
	}
	return report, nil
}

func effectiveCost(cost int) int {
	if cost <= 0 {
		return bcrypt.DefaultCost
	}
	return cost
}

func randomPassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*"
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

func publicReason(err error) string {
	if errors.Is(err, account.ErrUsernameTaken) {
		return "username already belongs to a different local account"
	}
	if errors.Is(err, account.ErrNotFound) {
		return "local account disappeared during sync"
	}
	return "local account update failed"
}

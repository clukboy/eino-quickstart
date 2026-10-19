package work

import (
	"context"
	"eino-quickstart/internal/platform/im/wechat"
	"eino-quickstart/internal/platform/usersource"
)

type UserSync struct {
	client *wechat.Client
}

func NewUserSync(client *wechat.Client) *UserSync {
	return &UserSync{
		client: client,
	}
}

func (s *UserSync) Fetch(ctx context.Context) ([]*usersource.User, error) {
	var cursor string
	var limit int = 10000
	users := make([]*usersource.User, 0)
	work, err := s.client.GetWork("contact")
	if err != nil {
		return nil, err
	}
	for {
		resp, err := work.User.ListID(ctx, cursor, limit)
		if err != nil {
			return nil, err
		}
		cursor = resp.NextCursor
		for _, u := range resp.DeptUser {
			users = append(users, &usersource.User{
				ExternalID: u.UserID,
			})
		}
		if len(resp.DeptUser) < limit {
			break
		}
	}
	return users, nil
}

func (s *UserSync) FetchByExternalID(ctx context.Context, externalID string) (*usersource.User, error) {
	work, err := s.client.GetWork("work")
	if err != nil {
		return nil, err
	}
	u, err := work.User.Get(ctx, externalID)
	if err != nil {
		return nil, err
	}
	return &usersource.User{
		ExternalID: u.UserID,
		Username:   u.UserID,
		Nickname:   u.Name,
	}, nil
}
 
package usersync

import (
	"context"
	"strings"
	"testing"

	"eino-quickstart/internal/platform/usersource"
)

// failingSource 复现线上那次 400：浏览器在 ~30s 取消请求后，补全调用返回
// (nil, context.Canceled)。
type failingSource struct {
	users []*usersource.User
	err   error
}

func (f *failingSource) Fetch(context.Context) ([]*usersource.User, error) { return f.users, nil }

func (f *failingSource) FetchByExternalID(context.Context, string) (*usersource.User, error) {
	return nil, f.err
}

var _ usersource.Source = (*failingSource)(nil)

// TestSyncReportsEnrichmentFailureInsteadOfPanicking 守住 service.go 里那个
// 「把补全结果写回循环变量」的坑：出错时补全返回 nil，若直接覆盖 u，下面错误
// 分支的 u.ExternalID 就是 nil 解引用 —— 线上表现是 RecoverHandler 打栈 +
// 400，而不是一条「这个成员失败了」的记录。
func TestSyncReportsEnrichmentFailureInsteadOfPanicking(t *testing.T) {
	service, err := NewService(newFakeStore(), &failingSource{
		users: []*usersource.User{{ExternalID: "u-1", Username: "alice", Nickname: "Alice"}},
		err:   context.Canceled,
	}, 4)
	if err != nil {
		t.Fatal(err)
	}

	report, err := service.Sync(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Total != 1 || report.Failed != 1 || report.Created != 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Errors) != 1 || report.Errors[0].ExternalID != "u-1" {
		t.Fatalf("errors = %+v", report.Errors)
	}
	if !strings.Contains(report.Errors[0].Reason, context.Canceled.Error()) {
		t.Fatalf("reason = %q", report.Errors[0].Reason)
	}
}

package feedback

import (
	"eino-quickstart/ent"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

func fail(err error) error {
	if ent.IsNotFound(err) {
		return httpx.NotFound("feedback not found")
	}
	return httpx.Internal("feedback operation failed")
}
func str(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func dto(x *ent.Feedback) *types.FeedbackResp {
	sid := ""
	if x.SessionID != nil {
		sid = *x.SessionID
	}
	return &types.FeedbackResp{ID: x.ID, RunID: x.RunID, SessionID: sid, Subject: x.Subject, Rating: x.Rating, Comment: x.Comment, Label: x.Label, Note: str(x.Note), CreatedAt: x.CreatedAt.UnixMilli()}
}

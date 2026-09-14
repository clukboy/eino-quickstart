package approver

import (
	"time"

	"eino-quickstart/internal/platform/persistence/approval"
	"eino-quickstart/internal/transport/restapi/internal/types"
)

// approvalDTO maps the persistence record onto the wire type.
//
// They are kept separate on purpose. approval.Request is internal — it carries
// RunID, TurnID and ArgumentsHash tagged `json:"-"` — while types.ApprovalResp
// is the published contract generated from restapi.api. Keep both in sync.
func approvalDTO(record *approval.Request) *types.ApprovalResp {
	if record == nil {
		return nil
	}

	resp := &types.ApprovalResp{
		ID:          record.ID,
		SessionID:   record.SessionID,
		Tool:        record.Tool,
		Status:      record.Status,
		RequestedBy: record.RequestedBy,
		CreatedAt:   record.CreatedAt.Format(time.RFC3339Nano),
	}

	if record.DisplayArguments != nil {
		resp.DisplayArguments = *record.DisplayArguments
	}
	if record.DecidedBy != nil {
		resp.DecidedBy = *record.DecidedBy
	}
	if record.CheckpointID != nil {
		resp.CheckpointID = *record.CheckpointID
	}
	if record.InterruptID != nil {
		resp.InterruptID = *record.InterruptID
	}
	if record.ExpiresAt != nil {
		resp.ExpiresAt = record.ExpiresAt.Format(time.RFC3339Nano)
	}

	return resp
}

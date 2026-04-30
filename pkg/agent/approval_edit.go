package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/andrewneudegg/lab/pkg/eventlog"
	"github.com/andrewneudegg/lab/pkg/id"
	approvalstore "github.com/andrewneudegg/lab/pkg/tools/approval"
)

func (o *Orchestrator) EditApprovalArgs(ctx context.Context, approvalID string, args json.RawMessage) (string, error) {
	return o.editApprovalArgsWithActor(ctx, approvalID, args, "human")
}

func (o *Orchestrator) editApprovalArgsWithActor(ctx context.Context, approvalID string, args json.RawMessage, actor string) (string, error) {
	actor = firstNonEmptyString(strings.TrimSpace(actor), "human")
	req, err := o.approvals.Load(approvalID)
	if err != nil {
		return "", err
	}
	if req.Status != approvalstore.StatusPending {
		return "approval is already " + req.Status, nil
	}
	cleanArgs, err := strictJSONObjectArgs(args)
	if err != nil {
		return "", err
	}
	if schema, ok := o.toolSchema(req.Tool); ok {
		if issues := validateJSONAgainstSchema("args", cleanArgs, schema); len(issues) > 0 {
			return "", fmt.Errorf("approval args failed schema validation: %s", strings.Join(issues, "; "))
		}
	}
	oldHash := rawJSONHash(req.Args)
	newHash := rawJSONHash(cleanArgs)
	req.Args = cleanArgs
	req.Reason = appendApprovalReason(req.Reason, "args edited by "+actor)
	if err := o.approvals.Save(req); err != nil {
		return "", err
	}
	_ = o.events.Append(ctx, eventlog.Event{
		ID:     id.New("evt"),
		Type:   "approval.edited",
		Actor:  actor,
		TaskID: req.TaskID,
		Payload: eventlog.Payload(map[string]any{
			"approval_id":     req.ID,
			"tool":            req.Tool,
			"old_args_sha256": oldHash,
			"new_args_sha256": newHash,
		}),
	})
	return fmt.Sprintf("Updated args for pending approval %s.", approvalID), nil
}

func strictJSONObjectArgs(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("args JSON object is required")
	}
	if !bytes.HasPrefix(raw, []byte("{")) {
		return nil, fmt.Errorf("args must be a JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("args are not valid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("args must contain exactly one JSON object")
	}
	return append(json.RawMessage(nil), raw...), nil
}

func rawJSONHash(raw json.RawMessage) string {
	sum := sha256.Sum256(bytes.TrimSpace(raw))
	return hex.EncodeToString(sum[:])
}

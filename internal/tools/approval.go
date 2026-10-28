package tools

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/signed"
	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	approvalRequestID = "approve"
	approvalKeyLabel  = "zapgate approval state v1"
	acceptAction      = "accept"
)

type approvalVerdict string

const (
	ownerApproved            approvalVerdict = "owner_approved"
	clientWithoutElicitation approvalVerdict = "client_without_elicitation"
	accessTrustsApp          approvalVerdict = "access_trusts_app"
)

var (
	errApprovalMismatch  = errors.New("the approval does not match this action: the arguments changed after the owner was asked; call the tool again to ask the owner about the new action")
	errApprovalMalformed = errors.New("the approval state is not valid; call the tool again without requestState")
	errApprovalUsed      = errors.New("this approval was already used; call the tool again to ask the owner again")
	errApprovalMissing   = errors.New("the answer of the owner is missing; call the tool again to ask the owner again")
)

type action struct {
	tool      string
	grantID   int64
	trustsApp bool
	fields    []string
	question  string
	undone    string
}

func (a action) digest() string {
	parts := append([]string{a.tool, strconv.FormatInt(a.grantID, 10)}, a.fields...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type approvalState struct {
	Grant   int64  `json:"g"`
	Action  string `json:"a"`
	Expires int64  `json:"e"`
	Nonce   string `json:"n"`
}

type approvals struct {
	signer signed.Signer
	clock  clock.Clock
	ttl    time.Duration
	store  *store.Store
}

func newApprovals(signingKey []byte, c clock.Clock, ttl time.Duration, st *store.Store) *approvals {
	return &approvals{signer: signed.NewSigner(signingKey, approvalKeyLabel), clock: c, ttl: ttl, store: st}
}

func (a *approvals) gate(ctx context.Context, req *mcp.CallToolRequest, act action) (*mcp.CallToolResult, approvalVerdict, error) {
	if act.trustsApp {
		return nil, accessTrustsApp, nil
	}
	if !clientCanElicit(req) {
		return nil, clientWithoutElicitation, nil
	}
	if req.Params.RequestState == "" {
		return a.ask(act), "", nil
	}
	if err := a.check(ctx, req.Params.RequestState, act); err != nil {
		return nil, "", err
	}
	answer, ok := req.Params.InputResponses[approvalRequestID].(*mcp.ElicitResult)
	if !ok {
		return nil, "", errApprovalMissing
	}
	if answer.Action != acceptAction {
		return nil, "", fmt.Errorf("%w (%s): %s", ErrDeclined, answer.Action, act.undone)
	}
	return nil, ownerApproved, nil
}

func clientCanElicit(req *mcp.CallToolRequest) bool {
	caps := req.ClientCapabilities()
	return caps != nil && caps.Elicitation != nil
}

func (a *approvals) ask(act action) *mcp.CallToolResult {
	state := approvalState{
		Grant:   act.grantID,
		Action:  act.digest(),
		Expires: a.clock.Now().Add(a.ttl).UnixMilli(),
		Nonce:   rand.Text(),
	}
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{approvalRequestID: &mcp.ElicitParams{
			Message:         act.question,
			RequestedSchema: &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}},
		}},
		RequestState: a.signer.Sign(state),
	}
}

func (a *approvals) check(ctx context.Context, raw string, act action) error {
	state, err := a.verify(raw)
	if err != nil {
		return err
	}
	if state.Grant != act.grantID || state.Action != act.digest() {
		return errApprovalMismatch
	}
	now := a.clock.Now()
	if now.UnixMilli() > state.Expires {
		return fmt.Errorf("the approval expired: the owner must answer within %s; call the tool again to ask the owner again", a.ttl)
	}
	return a.spend(ctx, state, now)
}

func (a *approvals) verify(raw string) (approvalState, error) {
	var state approvalState
	if err := a.signer.Open(raw, &state); err != nil || state.Nonce == "" {
		return approvalState{}, errApprovalMalformed
	}
	return state, nil
}

func (a *approvals) spend(ctx context.Context, state approvalState, now time.Time) error {
	fresh, err := a.store.SpendApproval(ctx, state.Nonce, time.UnixMilli(state.Expires), now)
	if err != nil {
		return fmt.Errorf("cannot record the use of the approval: %w", err)
	}
	if !fresh {
		return errApprovalUsed
	}
	return nil
}

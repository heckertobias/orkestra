package api

import (
	"context"

	"connectrpc.com/connect"

	sharedcompose "github.com/heckertobias/orkestra/internal/shared/compose"
	orkestraV1 "github.com/heckertobias/orkestra/internal/shared/gen/orkestra/v1"
)

// ValidateCompose runs YAML-syntax and supported-field checks on a compose YAML
// without persisting anything.  Any authenticated user may call this.
func (h *StackServiceHandler) ValidateCompose(
	_ context.Context,
	req *connect.Request[orkestraV1.ValidateComposeRequest],
) (*connect.Response[orkestraV1.ValidateComposeResponse], error) {
	diags := sharedcompose.ValidateCompose(req.Msg.ComposeYaml)
	resp := &orkestraV1.ValidateComposeResponse{
		Diagnostics: make([]*orkestraV1.ValidateComposeDiagnostic, 0, len(diags)),
	}
	for _, d := range diags {
		resp.Diagnostics = append(resp.Diagnostics, &orkestraV1.ValidateComposeDiagnostic{
			Severity: string(d.Severity),
			Message:  d.Message,
		})
	}
	return connect.NewResponse(resp), nil
}

package tenantaccess

import (
	"context"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"

	"encore.app/wabantu/shared/types"
)

//encore:api auth method=POST path=/api/v1/admin/tenant-access-requests tag:super_admin
func AdminCreateRequest(ctx context.Context, p *CreateRequestParams) (*AccessRequest, error) {
	u, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	return CreateRequest(ctx, u.AccountID, p)
}

//encore:api auth method=GET path=/api/v1/admin/tenant-access-requests tag:super_admin
func AdminListRequests(ctx context.Context, p *ListByRequesterParams) (*ListByRequesterResponse, error) {
	u, err := requireSuperAdmin(ctx)
	if err != nil {
		return nil, err
	}
	tid := ""
	if p != nil {
		tid = p.TenantID
	}
	list, err := ListByRequester(ctx, u.AccountID, tid)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "list permintaan gagal"}
	}
	return &ListByRequesterResponse{Requests: list}, nil
}

//encore:api auth method=GET path=/api/v1/tenant-access-requests tag:owner
func ListTenantRequests(ctx context.Context) (*ListForTenantResponse, error) {
	u, err := requireTenantOwner(ctx)
	if err != nil {
		return nil, err
	}
	list, err := ListForTenant(ctx, u.TenantID)
	if err != nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "list permintaan gagal"}
	}
	return &ListForTenantResponse{Requests: list}, nil
}

//encore:api auth method=POST path=/api/v1/tenant-access-requests/:id/respond tag:owner
func RespondToRequest(ctx context.Context, id string, p *RespondParams) (*RespondResponse, error) {
	u, err := requireTenantOwner(ctx)
	if err != nil {
		return nil, err
	}
	req, err := Respond(ctx, id, u.AccountID, u.TenantID, p)
	if err != nil {
		return nil, err
	}
	return &RespondResponse{Request: *req}, nil
}

//encore:api auth method=POST path=/api/v1/tenant-access-requests/:id/revoke tag:owner
func RevokeRequest(ctx context.Context, id string) (*RevokeResponse, error) {
	u, err := requireTenantOwner(ctx)
	if err != nil {
		return nil, err
	}
	req, err := Revoke(ctx, id, u.AccountID, u.TenantID)
	if err != nil {
		return nil, err
	}
	return &RevokeResponse{Request: *req}, nil
}

func requireSuperAdmin(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	if u.Role != "super_admin" {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "super admin access required"}
	}
	return u, nil
}

func requireTenantOwner(ctx context.Context) (*types.AuthUser, error) {
	u, ok := auth.Data().(*types.AuthUser)
	if !ok || u == nil {
		return nil, &errs.Error{Code: errs.Unauthenticated, Message: "not authenticated"}
	}
	if u.Impersonating {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "owner access required"}
	}
	if u.Role != "owner" || u.TenantID == "" {
		return nil, &errs.Error{Code: errs.PermissionDenied, Message: "owner access required"}
	}
	return u, nil
}

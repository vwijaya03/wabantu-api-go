package events

import (
	"context"
	"fmt"
	"strings"

	appErrs "encore.app/wabantu/shared/errs"
)

type Therapy struct {
	ID           string `json:"id"`
	TherapyName  string `json:"therapyName"`
	Description  string `json:"description,omitempty"`
	Active       bool   `json:"active"`
	DisplayOrder int    `json:"displayOrder"`
}

type VolunteerRole struct {
	ID           string `json:"id"`
	RoleName     string `json:"roleName"`
	Active       bool   `json:"active"`
	DisplayOrder int    `json:"displayOrder"`
}

type Task struct {
	ID             string `json:"id"`
	TaskName       string `json:"taskName"`
	AssignmentType string `json:"assignmentType"`
	Active         bool   `json:"active"`
	DisplayOrder   int    `json:"displayOrder"`
}

type ListMasterParams struct {
	Q        string `query:"q"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
	ActiveOnly bool `query:"activeOnly"`
}

type ListTherapiesResponse struct {
	Items []Therapy `json:"items"`
	Total int       `json:"total"`
}

//encore:api auth method=GET path=/api/v1/events/masters/therapies
func ListTherapies(ctx context.Context, p *ListMasterParams) (*ListTherapiesResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	page, pageSize := paginate(p.Page, p.PageSize)
	off, lim := offsetLimit(page, pageSize)
	conds := []string{"deleted_at IS NULL"}
	args := []any{}
	i := 1
	if p.ActiveOnly {
		conds = append(conds, "is_active = true")
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		conds = append(conds, fmt.Sprintf("therapy_name ILIKE $%d", i))
		args = append(args, "%"+q+"%")
		i++
	}
	where := strings.Join(conds, " AND ")
	var total int
	if err := ts.QueryRowContext(ctx, `SELECT COUNT(*) FROM evt_therapy WHERE `+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, lim, off)
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, therapy_name, COALESCE(description,''), is_active, display_order
		FROM evt_therapy WHERE %s ORDER BY display_order, therapy_name LIMIT $%d OFFSET $%d`,
		where, i, i+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var items []Therapy
	for rows.Next() {
		var t Therapy
		if err := rows.Scan(&t.ID, &t.TherapyName, &t.Description, &t.Active, &t.DisplayOrder); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		items = append(items, t)
	}
	if items == nil {
		items = []Therapy{}
	}
	return &ListTherapiesResponse{Items: items, Total: total}, nil
}

type UpsertTherapyParams struct {
	TherapyName  string `json:"therapyName"`
	Description  string `json:"description,omitempty"`
	Active       *bool  `json:"active,omitempty"`
	DisplayOrder int    `json:"displayOrder"`
}

//encore:api auth method=POST path=/api/v1/events/masters/therapies tag:owner
func CreateTherapy(ctx context.Context, p *UpsertTherapyParams) (*Therapy, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.TherapyName)
	if name == "" {
		return nil, appErrs.BadRequest("nama terapi wajib diisi")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	var id string
	err = ts.QueryRowContext(ctx, `
		INSERT INTO evt_therapy (therapy_name, description, is_active, display_order)
		VALUES ($1,$2,$3,$4) RETURNING id::text`,
		name, strings.TrimSpace(p.Description), active, p.DisplayOrder,
	).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "therapy", id, "create", nil, p)
	return &Therapy{ID: id, TherapyName: name, Description: p.Description, Active: active, DisplayOrder: p.DisplayOrder}, nil
}

//encore:api auth method=PUT path=/api/v1/events/masters/therapies/:id tag:owner
func UpdateTherapy(ctx context.Context, id string, p *UpsertTherapyParams) (*Therapy, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	name := strings.TrimSpace(p.TherapyName)
	if name == "" {
		return nil, appErrs.BadRequest("nama terapi wajib diisi")
	}
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	_, err = ts.ExecContext(ctx, `
		UPDATE evt_therapy SET therapy_name=$1, description=$2, is_active=$3, display_order=$4, updated_at=now()
		WHERE id=$5::uuid AND deleted_at IS NULL`, name, strings.TrimSpace(p.Description), active, p.DisplayOrder, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "therapy", id, "update", nil, p)
	return &Therapy{ID: id, TherapyName: name, Description: p.Description, Active: active, DisplayOrder: p.DisplayOrder}, nil
}

//encore:api auth method=DELETE path=/api/v1/events/masters/therapies/:id tag:owner
func DeleteTherapy(ctx context.Context, id string) error {
	u, err := mustUser(ctx)
	if err != nil {
		return err
	}
	if err := assertOwner(u); err != nil {
		return err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	_, err = ts.ExecContext(ctx, `UPDATE evt_therapy SET deleted_at=now() WHERE id=$1::uuid AND deleted_at IS NULL`, id)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "therapy", id, "delete", nil, nil)
	return nil
}

type ListVolunteerRolesResponse struct {
	Items []VolunteerRole `json:"items"`
	Total int             `json:"total"`
}

//encore:api auth method=GET path=/api/v1/events/masters/volunteer-roles
func ListVolunteerRoles(ctx context.Context, p *ListMasterParams) (*ListVolunteerRolesResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	page, pageSize := paginate(p.Page, p.PageSize)
	off, lim := offsetLimit(page, pageSize)
	conds := []string{"deleted_at IS NULL"}
	args := []any{}
	i := 1
	if p.ActiveOnly {
		conds = append(conds, "is_active = true")
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		conds = append(conds, fmt.Sprintf("role_name ILIKE $%d", i))
		args = append(args, "%"+q+"%")
		i++
	}
	where := strings.Join(conds, " AND ")
	var total int
	if err := ts.QueryRowContext(ctx, `SELECT COUNT(*) FROM evt_volunteer_role WHERE `+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, lim, off)
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, role_name, is_active, display_order
		FROM evt_volunteer_role WHERE %s ORDER BY display_order, role_name LIMIT $%d OFFSET $%d`,
		where, i, i+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var items []VolunteerRole
	for rows.Next() {
		var r VolunteerRole
		if err := rows.Scan(&r.ID, &r.RoleName, &r.Active, &r.DisplayOrder); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		items = append(items, r)
	}
	if items == nil {
		items = []VolunteerRole{}
	}
	return &ListVolunteerRolesResponse{Items: items, Total: total}, nil
}

type UpsertVolunteerRoleParams struct {
	RoleName     string `json:"roleName"`
	Active       *bool  `json:"active,omitempty"`
	DisplayOrder int    `json:"displayOrder"`
}

//encore:api auth method=POST path=/api/v1/events/masters/volunteer-roles tag:owner
func CreateVolunteerRole(ctx context.Context, p *UpsertVolunteerRoleParams) (*VolunteerRole, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.RoleName)
	if name == "" {
		return nil, appErrs.BadRequest("nama peran wajib diisi")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	var id string
	err = ts.QueryRowContext(ctx, `
		INSERT INTO evt_volunteer_role (role_name, is_active, display_order) VALUES ($1,$2,$3) RETURNING id::text`,
		name, active, p.DisplayOrder).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "volunteer_role", id, "create", nil, p)
	return &VolunteerRole{ID: id, RoleName: name, Active: active, DisplayOrder: p.DisplayOrder}, nil
}

//encore:api auth method=PUT path=/api/v1/events/masters/volunteer-roles/:id tag:owner
func UpdateVolunteerRole(ctx context.Context, id string, p *UpsertVolunteerRoleParams) (*VolunteerRole, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.RoleName)
	if name == "" {
		return nil, appErrs.BadRequest("nama peran wajib diisi")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	_, err = ts.ExecContext(ctx, `
		UPDATE evt_volunteer_role SET role_name=$1, is_active=$2, display_order=$3, updated_at=now()
		WHERE id=$4::uuid AND deleted_at IS NULL`, name, active, p.DisplayOrder, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "volunteer_role", id, "update", nil, p)
	return &VolunteerRole{ID: id, RoleName: name, Active: active, DisplayOrder: p.DisplayOrder}, nil
}

//encore:api auth method=DELETE path=/api/v1/events/masters/volunteer-roles/:id tag:owner
func DeleteVolunteerRole(ctx context.Context, id string) error {
	u, err := mustUser(ctx)
	if err != nil {
		return err
	}
	if err := assertOwner(u); err != nil {
		return err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	_, err = ts.ExecContext(ctx, `UPDATE evt_volunteer_role SET deleted_at=now() WHERE id=$1::uuid AND deleted_at IS NULL`, id)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "volunteer_role", id, "delete", nil, nil)
	return nil
}

type ListTasksResponse struct {
	Items []Task `json:"items"`
	Total int    `json:"total"`
}

//encore:api auth method=GET path=/api/v1/events/masters/tasks
func ListTasks(ctx context.Context, p *ListMasterParams) (*ListTasksResponse, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	page, pageSize := paginate(p.Page, p.PageSize)
	off, lim := offsetLimit(page, pageSize)
	conds := []string{"deleted_at IS NULL"}
	args := []any{}
	i := 1
	if p.ActiveOnly {
		conds = append(conds, "is_active = true")
	}
	if q := strings.TrimSpace(p.Q); q != "" {
		conds = append(conds, fmt.Sprintf("task_name ILIKE $%d", i))
		args = append(args, "%"+q+"%")
		i++
	}
	where := strings.Join(conds, " AND ")
	var total int
	if err := ts.QueryRowContext(ctx, `SELECT COUNT(*) FROM evt_task WHERE `+where, args...).Scan(&total); err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	args = append(args, lim, off)
	rows, err := ts.QueryContext(ctx, fmt.Sprintf(`
		SELECT id::text, task_name, assignment_type, is_active, display_order
		FROM evt_task WHERE %s ORDER BY display_order, task_name LIMIT $%d OFFSET $%d`,
		where, i, i+1), args...)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	defer rows.Close()
	var items []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.TaskName, &t.AssignmentType, &t.Active, &t.DisplayOrder); err != nil {
			return nil, appErrs.Internal(err.Error())
		}
		items = append(items, t)
	}
	if items == nil {
		items = []Task{}
	}
	return &ListTasksResponse{Items: items, Total: total}, nil
}

type UpsertTaskParams struct {
	TaskName       string `json:"taskName"`
	AssignmentType string `json:"assignmentType"`
	Active         *bool  `json:"active,omitempty"`
	DisplayOrder   int    `json:"displayOrder"`
}

//encore:api auth method=POST path=/api/v1/events/masters/tasks tag:owner
func CreateTask(ctx context.Context, p *UpsertTaskParams) (*Task, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.TaskName)
	if name == "" {
		return nil, appErrs.BadRequest("nama tugas wajib diisi")
	}
	at := strings.ToUpper(strings.TrimSpace(p.AssignmentType))
	if at == "" {
		at = "PER_HOUR"
	}
	if at != "PER_HOUR" && at != "PER_SESSION" && at != "FIXED" {
		return nil, appErrs.BadRequest("assignment type tidak valid")
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	var id string
	err = ts.QueryRowContext(ctx, `
		INSERT INTO evt_task (task_name, assignment_type, is_active, display_order)
		VALUES ($1,$2,$3,$4) RETURNING id::text`, name, at, active, p.DisplayOrder).Scan(&id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "task", id, "create", nil, p)
	return &Task{ID: id, TaskName: name, AssignmentType: at, Active: active, DisplayOrder: p.DisplayOrder}, nil
}

//encore:api auth method=PUT path=/api/v1/events/masters/tasks/:id tag:owner
func UpdateTask(ctx context.Context, id string, p *UpsertTaskParams) (*Task, error) {
	u, err := mustUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := assertOwner(u); err != nil {
		return nil, err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	name := strings.TrimSpace(p.TaskName)
	at := strings.ToUpper(strings.TrimSpace(p.AssignmentType))
	active := true
	if p.Active != nil {
		active = *p.Active
	}
	_, err = ts.ExecContext(ctx, `
		UPDATE evt_task SET task_name=$1, assignment_type=$2, is_active=$3, display_order=$4, updated_at=now()
		WHERE id=$5::uuid AND deleted_at IS NULL`, name, at, active, p.DisplayOrder, id)
	if err != nil {
		return nil, appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "task", id, "update", nil, p)
	return &Task{ID: id, TaskName: name, AssignmentType: at, Active: active, DisplayOrder: p.DisplayOrder}, nil
}

//encore:api auth method=DELETE path=/api/v1/events/masters/tasks/:id tag:owner
func DeleteTask(ctx context.Context, id string) error {
	u, err := mustUser(ctx)
	if err != nil {
		return err
	}
	if err := assertOwner(u); err != nil {
		return err
	}
	ts, err := openTenant(ctx, u.TenantSchema)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	_, err = ts.ExecContext(ctx, `UPDATE evt_task SET deleted_at=now() WHERE id=$1::uuid AND deleted_at IS NULL`, id)
	if err != nil {
		return appErrs.Internal(err.Error())
	}
	auditEvent(ctx, ts, u, "task", id, "delete", nil, nil)
	return nil
}

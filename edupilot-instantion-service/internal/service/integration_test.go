//go:build integration

// Run with a disposable Postgres:
//
//	DB_HOST=localhost DB_PORT=55432 DB_USER=postgres DB_PASSWORD=postgres \
//	DB_NAME=edupilot_instantion_db go test -tags integration ./internal/service/
package service

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/client"
	"github.com/edupilot-instantion-service/internal/config"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/google/uuid"
)

type fakeProvisioner struct {
	id      uint
	deleted []uint
}

func (f *fakeProvisioner) CreateUser(_ context.Context, _ client.CreateUserRequest) (*client.AuthUser, error) {
	return &client.AuthUser{ID: f.id}, nil
}
func (f *fakeProvisioner) DeleteUser(_ context.Context, id uint) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func wantStatus(t *testing.T, err error, status int) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Status != status {
		t.Fatalf("want status %d, got %v", status, err)
	}
}

func TestIntegration(t *testing.T) {
	if os.Getenv("DB_HOST") == "" {
		t.Skip("DB_HOST not set")
	}
	config.InitLogger()
	config.ConnectDB()
	ctx := context.Background()

	// Tenant resolution via customer_id fallback.
	school, err := ResolveTenantSchool(ctx, 5)
	if err != nil || school == nil {
		t.Fatalf("resolve: %v %v", school, err)
	}
	_, err = CreateSchool(ctx, 5, dto.SchoolRequest{Code: "S-new", Name: "x", SchoolLevel: "SD"})
	wantStatus(t, err, http.StatusConflict)

	// Teachers.
	req := &dto.TeacherRequest{FullName: "Budi", EmployeeCode: "G1"}
	_, _, err = CreateTeacher(ctx, school.ID, "", req)
	wantStatus(t, err, http.StatusConflict) // legacy G1 still present (oldest kept)

	req = &dto.TeacherRequest{FullName: "Budi Santoso", EmployeeCode: "G9"}
	created, _, err := CreateTeacher(ctx, school.ID, "", req)
	if err != nil {
		t.Fatal(err)
	}
	bad := &dto.TeacherRequest{FullName: "X", EmployeeCode: "G10", Schedules: []dto.TeacherScheduleReq{{
		SubjectID: dto.OptionalUUID{UUID: uuid.New(), Valid: true}, ClassID: dto.OptionalUUID{UUID: uuid.New(), Valid: true},
		DayOfWeek: 1, StartTime: "07:00", EndTime: "08:00"}}}
	_, _, err = CreateTeacher(ctx, school.ID, "", bad)
	wantStatus(t, err, http.StatusBadRequest) // foreign subject

	list, total, err := ListTeachers(ctx, school.ID, dto.ParsePageQuery("1", "10", "santoso"))
	if err != nil || total != 1 || list[0].ID != created.ID {
		t.Fatalf("list: %v %d %v", list, total, err)
	}
	_, err = GetTeacher(ctx, uuid.New(), created.ID)
	wantStatus(t, err, http.StatusNotFound) // other tenant

	upd := &dto.TeacherRequest{FullName: "Budi S", EmployeeCode: "G2"}
	_, err = UpdateTeacher(ctx, school.ID, created.ID, upd)
	wantStatus(t, err, http.StatusConflict)

	if err := DeleteTeacher(ctx, school.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	// Code reusable after soft delete (partial unique index).
	if _, _, err := CreateTeacher(ctx, school.ID, "", &dto.TeacherRequest{FullName: "Baru", EmployeeCode: "G9"}); err != nil {
		t.Fatal(err)
	}

	// Import (dry run then real) using the template.
	buf, _ := BuildImportTemplate()
	res, err := ImportTeachers(ctx, school.ID, "t.xlsx", int64(buf.Len()), buf, true)
	if err == nil {
		t.Fatalf("empty import should fail, got %+v", res)
	}

	// Admin create with account: link conflict triggers compensation.
	if _, err := AdminLinkAccount(ctx, school.ID, 77); err != nil {
		t.Fatal(err)
	}
	_, err = AdminLinkAccount(ctx, school.ID, 77)
	wantStatus(t, err, http.StatusConflict)

	fp := &fakeProvisioner{id: 77}
	NewUserProvisioner = func() UserProvisioner { return fp }
	_, err = AdminCreateSchool(ctx, dto.AdminCreateSchoolRequest{
		SchoolRequest: dto.SchoolRequest{Code: "ADM1", Name: "Adm", SchoolLevel: "SMA"},
		Account:       &dto.AccountRequest{Email: "a@b.co", Password: "12345678", FullName: "A"},
	})
	wantStatus(t, err, http.StatusConflict)
	if len(fp.deleted) != 1 || fp.deleted[0] != 77 {
		t.Fatalf("compensation not called: %v", fp.deleted)
	}
	if _, err := AdminGetSchool(ctx, uuid.Nil); err == nil {
		t.Fatal("expected not found")
	}
	items, total, err := AdminListSchools(ctx, dto.ParsePageQuery("1", "10", ""), "active")
	if err != nil || total != 1 || items[0].AccountCount != 1 {
		t.Fatalf("admin list (rolled back school must be absent): %+v %d %v", items, total, err)
	}
	stats, err := AdminGetStats(ctx)
	if err != nil || stats.TotalSchools != 1 || stats.TotalTeachers != 4 {
		t.Fatalf("stats %+v %v", stats, err)
	}

	// Tenant resolution via institution_accounts.
	if s, _ := ResolveTenantSchool(ctx, 77); s == nil || s.ID != school.ID {
		t.Fatal("resolve via institution account failed")
	}
}

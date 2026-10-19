package service

import (
	"strings"
	"testing"

	"github.com/edupilot-auth-service/internal/model"
	"github.com/edupilot-auth-service/internal/utils"
	"github.com/gin-gonic/gin/binding"
)

func TestFrontendBaseURL(t *testing.T) {
	t.Setenv("FRONTEND_URL", "http://schools.local/")
	t.Setenv("STUDENT_FRONTEND_URL", "")
	if got := frontendBaseURL(CustomerRoleStudents); got != "http://localhost:5175" {
		t.Fatalf("student default = %q", got)
	}
	t.Setenv("STUDENT_FRONTEND_URL", "http://students.local/")
	if got := frontendBaseURL(CustomerRoleStudents); got != "http://students.local" {
		t.Fatalf("student = %q", got)
	}
	for _, cr := range []string{"", RoleCustomer, RoleTeacher, "student"} {
		if got := frontendBaseURL(cr); got != "http://schools.local" {
			t.Errorf("customer role %q → %q", cr, got)
		}
	}
	if got := activationLink("abc", CustomerRoleStudents); got != "http://students.local/activate?token=abc" {
		t.Fatalf("activation link = %q", got)
	}
	if got := activationLink("abc", RoleTeacher); got != "http://schools.local/activate?token=abc" {
		t.Fatalf("teacher activation link = %q", got)
	}
}

func TestCustomerRoleOf(t *testing.T) {
	if customerRoleOf(nil) != "" || customerRoleOf(&model.User{}) != "" {
		t.Fatal("no customer row must give empty customer role")
	}
	u := &model.User{Customer: model.Customer{ID: 3, Role: CustomerRoleStudents}}
	if customerRoleOf(u) != CustomerRoleStudents {
		t.Fatal("customer role not read")
	}
	if dto := ToUserDTO(u); dto.Customer == nil || dto.Customer.Role != CustomerRoleStudents {
		t.Fatalf("UserDTO customer.role not populated: %+v", dto.Customer)
	}
}

func TestInviteRoles(t *testing.T) {
	if ur, cr, ok := inviteRoles("teacher"); !ok || ur != RoleTeacher || cr != RoleTeacher {
		t.Fatalf("teacher → %q %q %v", ur, cr, ok)
	}
	if ur, cr, ok := inviteRoles("student"); !ok || ur != RoleCustomer || cr != CustomerRoleStudents {
		t.Fatalf("student → %q %q %v", ur, cr, ok)
	}
	for _, bad := range []string{"", "admin", "customer", "students", "institution"} {
		if _, _, ok := inviteRoles(bad); ok {
			t.Errorf("invite role %q must be rejected", bad)
		}
	}
	if !isInvitableAccount(RoleTeacher, RoleTeacher) || !isInvitableAccount(RoleCustomer, CustomerRoleStudents) {
		t.Fatal("teacher/student must be re-invitable")
	}
	for _, c := range [][2]string{{RoleCustomer, RoleCustomer}, {RoleInstitution, CustomerRoleStudents}, {RoleAdmin, CustomerRoleStudents}} {
		if isInvitableAccount(c[0], c[1]) {
			t.Errorf("%v must not be re-invitable", c)
		}
	}
}

func TestRequestValidation(t *testing.T) {
	v := binding.Validator
	reg := func(app string) RegisterRequest {
		return RegisterRequest{Email: "a@b.co", Password: "12345678", FullName: "A", App: app}
	}
	for _, app := range []string{"", "schools", "students"} {
		if err := v.ValidateStruct(reg(app)); err != nil {
			t.Errorf("app %q rejected: %v", app, err)
		}
	}
	if err := v.ValidateStruct(reg("admin")); err == nil {
		t.Error("app admin accepted")
	}

	inv := func(role, school string) InviteUserRequest {
		return InviteUserRequest{Email: "a@b.co", FullName: "A", Role: role, SchoolName: school}
	}
	if err := v.ValidateStruct(inv("teacher", "SD 1")); err != nil {
		t.Errorf("teacher invite rejected: %v", err)
	}
	if err := v.ValidateStruct(inv("teacher", "")); err == nil {
		t.Error("teacher invite without school_name accepted")
	}
	if err := v.ValidateStruct(inv("student", "")); err != nil {
		t.Errorf("student invite rejected: %v", err)
	}
	if err := v.ValidateStruct(inv("customer", "x")); err == nil {
		t.Error("invite role customer accepted")
	}
}

func TestStudentEmails(t *testing.T) {
	body := utils.BuildStudentInviteEmail(`<b>x</b>`, "SMP 1", "http://x/activate?token=a&b", 72)
	if !strings.Contains(body, "akun siswa") || strings.Contains(body, "<b>x</b>") || !strings.Contains(body, "token=a&amp;b") {
		t.Fatal("student invite email wrong")
	}
	if !strings.Contains(utils.BuildResetPasswordEmail("http://x", true), "akun siswa") {
		t.Fatal("student reset email must say akun siswa")
	}
	if strings.Contains(utils.BuildResetPasswordEmail("http://x", false), "akun siswa") {
		t.Fatal("non-student reset email must not say akun siswa")
	}
}

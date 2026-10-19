package service

import (
	"testing"
	"time"
)

func TestCheckOTP(t *testing.T) {
	now := time.Now()
	code := 123456
	future := now.Add(time.Minute)
	past := now.Add(-time.Second)

	st := func(attempt int16, exp *time.Time) otpState {
		return otpState{ID: 1, EmailOtp: &code, EmailOtpExpiresAt: exp, EmailOtpAttempt: attempt}
	}

	if err := checkOTP(st(1, &future), "123456", now); err != nil {
		t.Fatalf("correct otp rejected: %v", err)
	}
	if err := checkOTP(st(5, &future), "123456", now); err != nil {
		t.Fatalf("5th attempt must still be allowed: %v", err)
	}
	if err := checkOTP(st(6, &future), "123456", now); err != ErrOTPTooManyAttempts {
		t.Fatalf("6th attempt must be blocked even with correct code, got %v", err)
	}
	if err := checkOTP(st(1, &future), "654321", now); err != ErrOTPInvalid {
		t.Fatalf("wrong otp: %v", err)
	}
	if err := checkOTP(st(1, &past), "123456", now); err != ErrOTPExpired {
		t.Fatalf("expired otp: %v", err)
	}
	if err := checkOTP(otpState{ID: 1, EmailOtpAttempt: 1}, "123456", now); err != ErrOTPInvalid {
		t.Fatalf("nil otp: %v", err)
	}
	small := 7
	s := otpState{ID: 1, EmailOtp: &small, EmailOtpExpiresAt: &future, EmailOtpAttempt: 1}
	if err := checkOTP(s, "000007", now); err != nil {
		t.Fatalf("zero-padded otp rejected: %v", err)
	}
}

func TestRoleRules(t *testing.T) {
	if canAssignRole(RoleAdmin, RoleAdmin) || canAssignRole(RoleAdmin, RoleSuperAdmin) {
		t.Fatal("admin must not grant admin/super_admin")
	}
	if !canAssignRole(RoleAdmin, RoleInstitution) || !canAssignRole(RoleSuperAdmin, RoleAdmin) {
		t.Fatal("expected allowed assignment")
	}
	if canAssignRole("", RoleAdmin) || !canAssignRole("", RoleInstitution) {
		t.Fatal("internal calls may only create institution/customer")
	}
	if canManageUser(RoleAdmin, RoleAdmin) || !canManageUser(RoleAdmin, RoleCustomer) || !canManageUser(RoleSuperAdmin, RoleSuperAdmin) {
		t.Fatal("unexpected canManageUser result")
	}
}

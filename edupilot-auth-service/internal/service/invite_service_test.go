package service

import (
	"strings"
	"testing"
	"time"

	"github.com/edupilot-auth-service/internal/utils"
)

func TestCheckActivation(t *testing.T) {
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Second)

	if err := checkActivation(&activationState{ID: 1, ActivationExpiresAt: &future}, now); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	cases := map[string]*activationState{
		"nil":       nil,
		"no id":     {ActivationExpiresAt: &future},
		"expired":   {ID: 1, ActivationExpiresAt: &past},
		"exact exp": {ID: 1, ActivationExpiresAt: &now},
		"no expiry": {ID: 1},
		"verified":  {ID: 1, ActivationExpiresAt: &future, EmailVerifiedAt: &now},
	}
	for name, st := range cases {
		if err := checkActivation(st, now); err != ErrActivationInvalid {
			t.Errorf("%s: expected ErrActivationInvalid, got %v", name, err)
		}
	}
}

func TestNormalizeActivationToken(t *testing.T) {
	raw, hash, err := newActivationToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 64 || hash != utils.HashToken(raw) || hash == raw {
		t.Fatalf("unexpected token/hash: %q %q", raw, hash)
	}
	if got, ok := normalizeActivationToken("  " + strings.ToUpper(raw) + "\n"); !ok || got != raw {
		t.Fatalf("normalize failed: %q %v", got, ok)
	}
	for _, bad := range []string{"", "abc", strings.Repeat("z", 64), raw + "0", "' OR 1=1 --"} {
		if _, ok := normalizeActivationToken(bad); ok {
			t.Errorf("token %q should be rejected", bad)
		}
	}
}

func TestActivationTTL(t *testing.T) {
	t.Setenv("ACTIVATION_TOKEN_TTL_HOURS", "")
	if ActivationTTL() != 72*time.Hour {
		t.Fatalf("default TTL = %v", ActivationTTL())
	}
	t.Setenv("ACTIVATION_TOKEN_TTL_HOURS", "24")
	if ActivationTTL() != 24*time.Hour {
		t.Fatalf("TTL = %v", ActivationTTL())
	}
	t.Setenv("ACTIVATION_TOKEN_TTL_HOURS", "-5")
	if ActivationTTL() != 72*time.Hour {
		t.Fatalf("negative TTL must fall back to default, got %v", ActivationTTL())
	}
}

func TestTeacherRole(t *testing.T) {
	if !IsValidRole(RoleTeacher) {
		t.Fatal("teacher must be a valid role")
	}
	for _, caller := range []string{"", RoleAdmin, RoleSuperAdmin} {
		if canAssignRole(caller, RoleTeacher) {
			t.Errorf("caller %q must not create/assign teacher outside the invite flow", caller)
		}
	}
	if !canManageUser(RoleAdmin, RoleTeacher) {
		t.Error("admin should manage teacher users")
	}
}

func TestInviteEmailEscaping(t *testing.T) {
	body := utils.BuildTeacherInviteEmail(`<script>x</script>`, `SD "A" & B`, "http://x/activate?token=a&b", 72)
	if strings.Contains(body, "<script>") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatal("name not escaped")
	}
	if !strings.Contains(body, "SD &#34;A&#34; &amp; B") {
		t.Fatal("school name not escaped")
	}
	if !strings.Contains(body, "token=a&amp;b") || !strings.Contains(body, "72 jam") {
		t.Fatal("link/ttl missing")
	}
}

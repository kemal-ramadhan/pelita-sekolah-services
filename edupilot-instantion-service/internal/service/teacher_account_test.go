package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/edupilot-instantion-service/internal/apperr"
	"github.com/edupilot-instantion-service/internal/client"
	"github.com/edupilot-instantion-service/internal/dto"
	"github.com/edupilot-instantion-service/internal/model"
)

func strp(s string) *string { return &s }

func TestEmailChangeLocked(t *testing.T) {
	uid := uint(3)
	now := time.Now()
	none := &model.Employees{Email: strp("a@x.id")}
	invited := &model.Employees{Email: strp("a@x.id"), UserID: &uid}
	active := &model.Employees{Email: strp("a@x.id"), UserID: &uid, ActivatedAt: &now}

	if emailChangeLocked(none, strp("b@x.id")) || emailChangeLocked(none, nil) {
		t.Error("status none: email must be free to change")
	}
	for name, e := range map[string]*model.Employees{"invited": invited, "active": active} {
		if emailChangeLocked(e, strp("a@x.id")) || emailChangeLocked(e, strp(" A@X.ID ")) {
			t.Errorf("%s: unchanged email must be allowed", name)
		}
		if !emailChangeLocked(e, strp("b@x.id")) || !emailChangeLocked(e, nil) {
			t.Errorf("%s: changed/cleared email must be locked", name)
		}
	}
}

func TestWantsInvite(t *testing.T) {
	f, tr := false, true
	cases := []struct {
		req  dto.TeacherRequest
		want bool
	}{
		{dto.TeacherRequest{}, false},
		{dto.TeacherRequest{Email: strp("a@x.id")}, true},
		{dto.TeacherRequest{Email: strp("a@x.id"), SendInvite: &tr}, true},
		{dto.TeacherRequest{Email: strp("a@x.id"), SendInvite: &f}, false},
		{dto.TeacherRequest{SendInvite: &tr}, false},
	}
	for i, c := range cases {
		if got := c.req.WantsInvite(); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}
}

func TestMapInviteErrorConflict(t *testing.T) {
	err := mapInviteError(&client.AuthError{Status: http.StatusConflict, Code: "EMAIL_TAKEN"})
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict || ae.Code != "EMAIL_IN_USE" {
		t.Fatalf("got %v", err)
	}
	err = mapInviteError(&client.AuthError{Status: http.StatusBadRequest, Message: "bad"})
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		t.Fatalf("got %v", err)
	}
}

func TestInviteWarning(t *testing.T) {
	if inviteWarning(nil) != "" || inviteWarning(&client.InvitedUser{EmailSent: true}) != "" {
		t.Error("no warning expected")
	}
	if inviteWarning(&client.InvitedUser{EmailSent: false}) == "" {
		t.Error("warning expected when email not sent")
	}
}

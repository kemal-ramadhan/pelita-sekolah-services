package model

import (
	"testing"
	"time"
)

func TestComputeAccountStatus(t *testing.T) {
	id, zero := uint(7), uint(0)
	now := time.Now()
	cases := []struct {
		userID    *uint
		activated *time.Time
		want      string
	}{
		{nil, nil, AccountStatusNone},
		{&zero, nil, AccountStatusNone},
		{nil, &now, AccountStatusNone},
		{&id, nil, AccountStatusInvited},
		{&id, &now, AccountStatusActive},
	}
	for i, c := range cases {
		if got := ComputeAccountStatus(c.userID, c.activated); got != c.want {
			t.Errorf("case %d: got %q want %q", i, got, c.want)
		}
	}
	e := Employees{UserID: &id}
	if err := e.AfterFind(nil); err != nil || e.AccountStatus != AccountStatusInvited {
		t.Fatalf("AfterFind: %q %v", e.AccountStatus, err)
	}
}

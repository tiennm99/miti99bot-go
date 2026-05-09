package modules

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestAuth_Permits(t *testing.T) {
	const owner int64 = 100
	const admin int64 = 200
	const stranger int64 = 999

	auth := Auth{
		BotOwnerID:   owner,
		AdminUserIDs: map[int64]bool{admin: true},
	}

	updateFrom := func(id int64) *models.Update {
		return &models.Update{Message: &models.Message{From: &models.User{ID: id}}}
	}

	cases := []struct {
		name    string
		v       Visibility
		update  *models.Update
		expect  bool
	}{
		{"public-no-message", VisibilityPublic, &models.Update{}, true},
		{"public-stranger", VisibilityPublic, updateFrom(stranger), true},
		{"protected-owner", VisibilityProtected, updateFrom(owner), true},
		{"protected-admin", VisibilityProtected, updateFrom(admin), true},
		{"protected-stranger", VisibilityProtected, updateFrom(stranger), false},
		{"private-owner", VisibilityPrivate, updateFrom(owner), true},
		{"private-admin", VisibilityPrivate, updateFrom(admin), false},
		{"private-stranger", VisibilityPrivate, updateFrom(stranger), false},
		{"protected-nil-message", VisibilityProtected, &models.Update{}, false},
		{"private-nil-from", VisibilityPrivate, &models.Update{Message: &models.Message{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.Permits(tc.v, tc.update)
			if got != tc.expect {
				t.Errorf("Permits(%v) = %v, want %v", tc.v, got, tc.expect)
			}
		})
	}
}

func TestAuth_ZeroDeniesAllGated(t *testing.T) {
	// Misconfigured deploy: zero-value Auth must deny every Protected/Private
	// command without panicking, so an unconfigured bot cannot be hijacked
	// just because an admin env var was forgotten.
	var auth Auth
	update := &models.Update{Message: &models.Message{From: &models.User{ID: 1}}}

	if !auth.Permits(VisibilityPublic, update) {
		t.Error("zero-Auth must still permit Public")
	}
	if auth.Permits(VisibilityProtected, update) {
		t.Error("zero-Auth must deny Protected")
	}
	if auth.Permits(VisibilityPrivate, update) {
		t.Error("zero-Auth must deny Private")
	}
}

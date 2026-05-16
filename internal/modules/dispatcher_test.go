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

func TestMatchCommand(t *testing.T) {
	mkUpdate := func(text string, entities ...models.MessageEntity) *models.Update {
		return &models.Update{
			Message: &models.Message{
				Text:     text,
				Entities: entities,
			},
		}
	}
	cmd := func(off, length int) models.MessageEntity {
		return models.MessageEntity{Type: models.MessageEntityTypeBotCommand, Offset: off, Length: length}
	}

	cases := []struct {
		name   string
		want   string
		update *models.Update
		expect bool
	}{
		{
			name:   "dm bare slash-help",
			want:   "help",
			update: mkUpdate("/help", cmd(0, 5)),
			expect: true,
		},
		{
			// The bug this fix addresses: group clients append @botname to
			// the entity. The upstream library's MatchTypeCommand misses this.
			name:   "group slash-help-at-botname",
			want:   "help",
			update: mkUpdate("/help@miti99bot", cmd(0, 15)),
			expect: true,
		},
		{
			name:   "group slash-help-at-botname with trailing arg",
			want:   "help",
			update: mkUpdate("/help@miti99bot arg", cmd(0, 15)),
			expect: true,
		},
		{
			name:   "different command no match",
			want:   "help",
			update: mkUpdate("/info", cmd(0, 5)),
			expect: false,
		},
		{
			name:   "different command with botname no match",
			want:   "help",
			update: mkUpdate("/info@miti99bot", cmd(0, 15)),
			expect: false,
		},
		{
			name:   "non-command entity ignored",
			want:   "help",
			update: mkUpdate("/help", models.MessageEntity{Type: models.MessageEntityTypeMention, Offset: 0, Length: 5}),
			expect: false,
		},
		{
			name:   "command not at start matches (lib parity)",
			want:   "help",
			update: mkUpdate("hi /help", cmd(3, 5)),
			expect: true,
		},
		{
			name:   "nil update",
			want:   "help",
			update: nil,
			expect: false,
		},
		{
			name:   "no message",
			want:   "help",
			update: &models.Update{},
			expect: false,
		},
		{
			name:   "no entities",
			want:   "help",
			update: mkUpdate("/help"),
			expect: false,
		},
		{
			name:   "out-of-bounds entity ignored",
			want:   "help",
			update: mkUpdate("/help", cmd(0, 999)),
			expect: false,
		},
		{
			name:   "zero-length entity ignored",
			want:   "help",
			update: mkUpdate("/help", cmd(0, 0)),
			expect: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchCommand(tc.want, tc.update)
			if got != tc.expect {
				t.Errorf("matchCommand(%q, ...) = %v, want %v", tc.want, got, tc.expect)
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

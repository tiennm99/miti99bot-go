package chathelper

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestSubjectFor(t *testing.T) {
	tests := []struct {
		name string
		msg  *models.Message
		want string
	}{
		{
			name: "nil message",
			msg:  nil,
			want: "",
		},
		{
			name: "private chat with From",
			msg: &models.Message{
				Chat: models.Chat{ID: 999, Type: models.ChatTypePrivate},
				From: &models.User{ID: 42},
			},
			want: "42",
		},
		{
			name: "private chat without From",
			msg: &models.Message{
				Chat: models.Chat{ID: 999, Type: models.ChatTypePrivate},
			},
			want: "",
		},
		{
			name: "group chat → chat id (ignores From)",
			msg: &models.Message{
				Chat: models.Chat{ID: -100, Type: models.ChatTypeGroup},
				From: &models.User{ID: 42},
			},
			want: "-100",
		},
		{
			name: "supergroup → chat id",
			msg: &models.Message{
				Chat: models.Chat{ID: -1001, Type: models.ChatTypeSupergroup},
				From: &models.User{ID: 42},
			},
			want: "-1001",
		},
		{
			name: "channel falls through to From.ID",
			msg: &models.Message{
				Chat: models.Chat{ID: -200, Type: models.ChatTypeChannel},
				From: &models.User{ID: 7},
			},
			want: "7",
		},
		{
			name: "channel without From",
			msg: &models.Message{
				Chat: models.Chat{ID: -200, Type: models.ChatTypeChannel},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SubjectFor(tt.msg); got != tt.want {
				t.Errorf("SubjectFor: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestArgAfterCommand(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"/cmd", ""},
		{"/cmd ", ""},
		{"/cmd  ", ""},
		{"/cmd word", "word"},
		{"/cmd  word  ", "word"},
		{"/cmd@bot word", "word"},
		{"/cmd two words", "two words"},
		{"/cmd  two   words  ", "two   words"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ArgAfterCommand(tt.in); got != tt.want {
				t.Errorf("ArgAfterCommand(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNowMillis(t *testing.T) {
	a := NowMillis()
	b := NowMillis()
	if b < a {
		t.Errorf("NowMillis went backwards: %d → %d", a, b)
	}
	if a < 1700000000000 {
		t.Errorf("NowMillis too small (not ms-epoch?): %d", a)
	}
}

func TestWinRate(t *testing.T) {
	tests := []struct {
		wins, played, want int
	}{
		{0, 0, 0},   // no games
		{0, 5, 0},   // 0%
		{5, 5, 100}, // 100%
		{2, 3, 67},  // round-half-up: 66.67% → 67% (NOT 66%)
		{1, 3, 33},  // 33.33% → 33%
		{1, 6, 17},  // 16.67% → 17%
		{1, 2, 50},  // exact 50%
		{3, 4, 75},  // exact 75%
		// negative played guards against caller bugs.
		{1, -1, 0},
	}
	for _, tt := range tests {
		got := WinRate(tt.wins, tt.played)
		if got != tt.want {
			t.Errorf("WinRate(%d,%d) = %d, want %d", tt.wins, tt.played, got, tt.want)
		}
	}
}

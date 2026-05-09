package util_test

import (
	"context"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/tiennm99/miti99bot-go/internal/modules"
	"github.com/tiennm99/miti99bot-go/internal/modules/util"
	"github.com/tiennm99/miti99bot-go/internal/storage"
	"github.com/tiennm99/miti99bot-go/internal/testutil"
)

// installUtil builds a registry with the util module + auth that admits the
// supplied owner so /stickerid (private) dispatches.
func installUtil(t *testing.T, ownerID int64) *testutil.RecordingBot {
	t.Helper()
	rb := testutil.NewRecordingBot(t)
	reg, err := modules.Build([]string{"util"},
		map[string]modules.Factory{"util": util.New},
		storage.NewMemoryProvider(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	modules.Install(rb.Bot, reg, modules.Auth{BotOwnerID: ownerID})
	return rb
}

func TestInfo_PrivateChat(t *testing.T) {
	rb := installUtil(t, 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(42, "/info"))

	got := rb.LastSent().Text()
	for _, want := range []string{"chat id: 42", "thread id: n/a", "sender id: 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("info reply missing %q; got %q", want, got)
		}
	}
}

func TestInfo_GroupChat(t *testing.T) {
	rb := installUtil(t, 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewGroupMessage(-100, 7, "/info"))

	got := rb.LastSent().Text()
	for _, want := range []string{"chat id: -100", "sender id: 7"} {
		if !strings.Contains(got, want) {
			t.Errorf("info reply missing %q; got %q", want, got)
		}
	}
}

func TestInfo_ChannelMessageNoFrom(t *testing.T) {
	rb := installUtil(t, 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewChannelMessage(-200, "/info"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "sender id: n/a") {
		t.Errorf("info channel reply missing 'sender id: n/a'; got %q", got)
	}
}

func TestHelp_RendersHTML(t *testing.T) {
	rb := installUtil(t, 0)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(1, "/help"))

	calls := rb.Sent()
	if len(calls) == 0 {
		t.Fatal("/help produced no reply")
	}
	got := calls[len(calls)-1]
	if got.Form["parse_mode"] != string(models.ParseModeHTML) {
		t.Errorf("/help parse_mode = %q, want HTML", got.Form["parse_mode"])
	}
	if !strings.Contains(got.Text(), "<b>util</b>") {
		t.Errorf("/help body missing util section; got %q", got.Text())
	}
}

func TestStickerID_NoReply_ShowsUsage(t *testing.T) {
	rb := installUtil(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(999, "/stickerid"))

	got := rb.LastSent().Text()
	if !strings.Contains(got, "Reply to a sticker") {
		t.Errorf("stickerid usage missing; got %q", got)
	}
}

func TestStickerID_WithStickerReply_EchoesFileID(t *testing.T) {
	rb := installUtil(t, 999)
	upd := testutil.NewPrivateMessage(999, "/stickerid")
	upd.Message.ReplyToMessage = &models.Message{
		Sticker: &models.Sticker{
			FileID:       "AAA-file-id",
			FileUniqueID: "uniq",
			SetName:      "TestSet",
			Emoji:        "🎉",
		},
	}
	rb.Bot.ProcessUpdate(context.Background(), upd)

	got := rb.LastSent().Text()
	for _, want := range []string{"AAA-file-id", "uniq", "TestSet", "🎉"} {
		if !strings.Contains(got, want) {
			t.Errorf("stickerid reply missing %q; got %q", want, got)
		}
	}
}

func TestStickerID_DeniedToNonOwner(t *testing.T) {
	rb := installUtil(t, 999)
	rb.Bot.ProcessUpdate(context.Background(), testutil.NewPrivateMessage(7, "/stickerid"))
	if calls := rb.Sent(); len(calls) != 0 {
		t.Errorf("non-owner /stickerid replied: %+v", calls)
	}
}

package testutil

import (
	"context"
	"testing"

	"github.com/go-telegram/bot"
)

// Smoke test: the recording bot must capture an outbound SendMessage so
// downstream handler tests can rely on Sent() / AssertSentText.
func TestRecordingBot_CapturesSendMessage(t *testing.T) {
	rb := NewRecordingBot(t)

	_, err := rb.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: int64(42),
		Text:   "hello",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	calls := rb.Sent()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1: %+v", len(calls), calls)
	}
	if calls[0].Method != "sendMessage" {
		t.Errorf("method = %q, want sendMessage", calls[0].Method)
	}
	if calls[0].Text() != "hello" {
		t.Errorf("text = %q, want hello", calls[0].Text())
	}
	if calls[0].ChatID() != "42" {
		t.Errorf("chat_id = %q, want 42", calls[0].ChatID())
	}
}

func TestRecordingBot_AssertSentText(t *testing.T) {
	rb := NewRecordingBot(t)
	_, err := rb.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: int64(1),
		Text:   "Welcome to the bot",
	})
	if err != nil {
		t.Fatal(err)
	}
	rb.AssertSentText(t, "Welcome")
}

func TestRecordingBot_Reset(t *testing.T) {
	rb := NewRecordingBot(t)
	_, _ = rb.Bot.SendMessage(context.Background(), &bot.SendMessageParams{
		ChatID: int64(1), Text: "first",
	})
	rb.Reset()
	if got := len(rb.Sent()); got != 0 {
		t.Errorf("Sent() after Reset = %d, want 0", got)
	}
}

func TestUpdateBuilders_BotCommandEntity(t *testing.T) {
	tests := []struct {
		text string
		off  int
		ln   int
	}{
		{"/wordle", 0, 7},
		{"/wordle apple", 0, 7},
		{"/wordle@bot apple", 0, 7},
	}
	for _, tt := range tests {
		got := botCommandEntity(tt.text)
		if got.Offset != tt.off || got.Length != tt.ln {
			t.Errorf("botCommandEntity(%q) = (%d,%d), want (%d,%d)",
				tt.text, got.Offset, got.Length, tt.off, tt.ln)
		}
	}
}

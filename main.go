package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strconv"
	"time"

	gotgbot "github.com/PaulSonOfLars/gotgbot/v2"
)

const retryLimit = 10

func main() {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	token := os.Getenv("TELEGRAM_BOTAPI_TOKEN")
	if token == "" {
		panic("env TELEGRAM_BOTAPI_TOKEN is required")
	}

	logGroupID, _ := strconv.ParseInt(os.Getenv("LOG_GROUP_ID"), 10, 64)

	bot, err := gotgbot.NewBot(token, nil)
	if err != nil {
		panic("starting telegram bot: " + err.Error())
	}

	logBan := func(user *gotgbot.User) {}

	if logGroupID != 0 {
		slog.Debug("enabling ban journal", "journal_group", logGroupID)
		logBan = func(user *gotgbot.User) {
			msg := fmt.Sprintf(`User %d %q %q is_premium=%v has no username and was banned`,
				user.Id, user.FirstName, user.LastName, user.IsPremium)

			_, err := bot.SendMessage(logGroupID, msg, nil)
			if err != nil {
				slog.Error("unable to send log for banned user", "error", err)
			}
		}
	}

	allowedUpdates := []string{
		"chat_member",
	}

	var offset int64

	for retry := 0; retry < retryLimit; {
		slog.Debug("fetching updates",
			"offset", offset)
		updates, err := bot.GetUpdates(&gotgbot.GetUpdatesOpts{
			Offset:         offset,
			Timeout:        int64(4 * time.Minute / time.Second),
			AllowedUpdates: allowedUpdates,
			RequestOpts: &gotgbot.RequestOpts{
				Timeout: 5 * time.Minute,
			},
		})

		if errors.Is(err, context.DeadlineExceeded) {
			slog.Debug("polling timeout, next poll")
			continue
		}

		if err != nil {
			retry++
			dt := time.Duration(retry) * time.Second
			slog.Error("getting updates, sleeping and retrying",
				"error", err,
				"sleep", dt,
				"retry_attempt", retry)
			time.Sleep(dt)
		}
		retry = 0

		slog.Debug("got updates", "n_updates", len(updates))

		for _, update := range updates {
			offset = max(update.UpdateId, offset) + 1
			if update.ChatMember == nil || update.ChatMember.NewChatMember == nil {
				continue
			}

			newMember, ok := update.ChatMember.NewChatMember.(gotgbot.ChatMemberMember)
			if !ok {
				slog.Info("skipping event", "event_type", reflect.TypeOf(update.ChatMember.NewChatMember))
				continue
			}
			if user := newMember.User; user.Username == "" {
				lg := slog.With("user.id", user.Id,
					"user.first_name", user.FirstName,
					"user.last_name", user.LastName,
					"user.is_premium", user.IsPremium)

				lg.Info("got an anon user!")

				_, err := bot.BanChatMember(update.ChatMember.Chat.Id, user.Id, nil)
				if err != nil {
					lg.Error("unable to ban user",
						"error", err)
					continue
				}
				lg.Info("successfully banned a user")
				logBan(&user)
			}
		}
	}
}

func userLogFields(user *gotgbot.User) []any {
	return []any{
		"user.id", user.Id,
		"user.first_name", user.FirstName,
		"user.last_name", user.LastName,
		"user.is_premium", user.IsPremium,
	}
}

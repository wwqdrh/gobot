package discord

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/joho/godotenv"
)

var testDiscordBot *DiscordBot

func TestMain(m *testing.M) {
	if err := godotenv.Load("testdata/.env"); err != nil {
		log.Fatal(err)
		return
	}

	testDiscordBot = NewDiscordBot(os.Getenv("USER_AUTHORIZATION"),
		WithBotToken(os.Getenv("BOT_TOKEN")),
		WithGuilID(os.Getenv("GUILD_ID")),
		WithBotID(os.Getenv("COZE_BOT_ID")),
		WithProxyUrl(os.Getenv("PROXY_URL")),
		WithProxySecret(os.Getenv("PROXY_SECRET")),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go testDiscordBot.StartBot(ctx)
	<-testDiscordBot.started
	os.Exit(m.Run())
}

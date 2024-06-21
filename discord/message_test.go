package discord

import (
	"fmt"
	"testing"
)

func TestBotMessage(t *testing.T) {
	if testDiscordBot == nil {
		t.Error("未正确初始化")
		return
	}

	msgs, err := testDiscordBot.SendPlain("你好，你叫什么名字，你可以做些什么")
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(msgs)
}

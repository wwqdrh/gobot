package discord

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/wwqdrh/gobot/types"
)

func res2OpenAI(m *discordgo.MessageCreate) types.OpenAIChatCompletionResponse {
	if len(m.Embeds) != 0 {
		for _, embed := range m.Embeds {
			if embed.Image != nil && !strings.Contains(m.Content, embed.Image.URL) {
				if m.Content != "" {
					m.Content += "\n"
				}
				m.Content += fmt.Sprintf("%s\n![Image](%s)", embed.Image.URL, embed.Image.URL)
			}
		}
	}

	promptTokens := CountTokens(m.ReferencedMessage.Content)
	completionTokens := CountTokens(m.Content)

	return types.OpenAIChatCompletionResponse{
		ID:      m.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "Coze-Model",
		Choices: []types.OpenAIChoice{
			{
				Index: 0,
				Message: types.OpenAIMessage{
					Role:    "assistant",
					Content: m.Content,
				},
			},
		},
		Usage: types.OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// processMessage 提取并处理消息内容及其嵌入元素
func processMessageUpdate(m *discordgo.MessageUpdate) ReplyResp {
	var embedUrls []string
	for _, embed := range m.Embeds {
		if embed.Image != nil {
			embedUrls = append(embedUrls, embed.Image.URL)
		}
	}

	return ReplyResp{
		Content:   m.Content,
		EmbedUrls: embedUrls,
	}
}

func processMessageUpdateForOpenAI(m *discordgo.MessageUpdate) types.OpenAIChatCompletionResponse {
	if len(m.Embeds) != 0 {
		for _, embed := range m.Embeds {
			if embed.Image != nil && !strings.Contains(m.Content, embed.Image.URL) {
				if m.Content != "" {
					m.Content += "\n"
				}
				m.Content += fmt.Sprintf("%s\n![Image](%s)", embed.Image.URL, embed.Image.URL)
			}
		}
	}

	promptTokens := CountTokens(m.ReferencedMessage.Content)
	completionTokens := CountTokens(m.Content)

	return types.OpenAIChatCompletionResponse{
		ID:      m.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "Coze-Model",
		Choices: []types.OpenAIChoice{
			{
				Index: 0,
				Message: types.OpenAIMessage{
					Role:    "assistant",
					Content: m.Content,
				},
			},
		},
		Usage: types.OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

// processMessage 提取并处理消息内容及其嵌入元素
func processMessageCreate(m *discordgo.MessageCreate) ReplyResp {
	var embedUrls []string
	for _, embed := range m.Embeds {
		if embed.Image != nil {
			embedUrls = append(embedUrls, embed.Image.URL)
		}
	}

	return ReplyResp{
		Content:   m.Content,
		EmbedUrls: embedUrls,
	}
}

func processMessageUpdateForOpenAIImage(m *discordgo.MessageUpdate) types.OpenAIImagesGenerationResponse {
	return res2OpenAIImage(m.Content, m.Embeds)
}

func processMessageCreateForOpenAIImage(m *discordgo.MessageCreate) types.OpenAIImagesGenerationResponse {
	return res2OpenAIImage(m.Content, m.Embeds)
}

func res2OpenAIImage(content string, embeds []*discordgo.MessageEmbed) types.OpenAIImagesGenerationResponse {
	var response types.OpenAIImagesGenerationResponse

	for _, item := range CozeDailyLimitErrorMessages {
		if item == content {
			return types.OpenAIImagesGenerationResponse{
				Created:    time.Now().Unix(),
				Data:       response.Data,
				DailyLimit: true,
			}
		}
	}

	re := regexp.MustCompile(`]\((https?://\S+)\)`)
	subMatches := re.FindAllStringSubmatch(content, -1)

	if len(subMatches) == 0 && len(embeds) == 0 {
		response.Data = append(response.Data, &types.OpenAIImagesGenerationDataResponse{
			RevisedPrompt: content,
		})
	}

	for i, match := range subMatches {
		response.Data = append(response.Data, &types.OpenAIImagesGenerationDataResponse{
			URL:           match[i],
			RevisedPrompt: content,
		})
	}

	if len(embeds) != 0 {
		for _, embed := range embeds {
			if embed.Image != nil && !strings.Contains(content, embed.Image.URL) {
				//if m.Content != "" {
				//	m.Content += "\n"
				//}
				response.Data = append(response.Data, &types.OpenAIImagesGenerationDataResponse{
					URL:           embed.Image.URL,
					RevisedPrompt: content,
				})
			}
		}
	}

	return types.OpenAIImagesGenerationResponse{
		Created: time.Now().Unix(),
		Data:    response.Data,
	}
}

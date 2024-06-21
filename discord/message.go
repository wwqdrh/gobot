package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"

	"fmt"
	"regexp"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/wwqdrh/gokit/logger"
)

var RateLimitKeyExpirationDuration = 20 * time.Minute

var RequestOutTimeDuration = 5 * time.Minute

var NoAvailableUserAuthChan = make(chan string)
var CreateChannelRiskChan = make(chan string)

var NoAvailableUserAuthPreNotifyTime time.Time
var CreateChannelRiskPreNotifyTime time.Time

var BotConfigList []BotConfig

type ReplyResp struct {
	Content   string   `json:"content" swaggertype:"string" description:"回复内容"`
	EmbedUrls []string `json:"embedUrls" swaggertype:"array,string" description:"嵌入网址"`
}

type ChannelResp struct {
	Id   string `json:"id" swaggertype:"string" description:"频道ID"`
	Name string `json:"name" swaggertype:"string" description:"频道名称"`
}

type ChannelStopChan struct {
	Id    string `json:"id" `
	IsNew bool   `json:"IsNew"`
}

type ChannelReq struct {
	ParentId string `json:"parentId" swaggertype:"string" description:"父频道Id,为空时默认为创建父频道"`
	Type     int    `json:"type" swaggertype:"number" description:"类型:[0:文本频道,4:频道分类](其它枚举请查阅discord-api文档)"`
	Name     string `json:"name" swaggertype:"string" description:"频道名称"`
}

type ThreadResp struct {
	Id   string `json:"id" swaggertype:"string" description:"线程ID"`
	Name string `json:"name" swaggertype:"string" description:"线程名称"`
}

type ThreadReq struct {
	ChannelId       string `json:"channelId" swaggertype:"string" description:"频道Id"`
	Name            string `json:"name" swaggertype:"string" description:"线程名称"`
	ArchiveDuration int    `json:"archiveDuration" swaggertype:"number" description:"线程存档时间[分钟]"`
}

type OpenAIChatCompletionRequest struct {
	Model    string              `json:"model"`
	Stream   bool                `json:"stream"`
	Messages []OpenAIChatMessage `json:"messages"`
	OpenAIChatCompletionExtraRequest
}

type OpenAIChatCompletionExtraRequest struct {
	ChannelId *string `json:"channelId"`
}

type OpenAIChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type OpenAIErrorResponse struct {
	OpenAIError OpenAIError `json:"error"`
}

type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

type OpenAIChatCompletionResponse struct {
	ID                string         `json:"id"`
	Object            string         `json:"object"`
	Created           int64          `json:"created"`
	Model             string         `json:"model"`
	Choices           []OpenAIChoice `json:"choices"`
	Usage             OpenAIUsage    `json:"usage"`
	SystemFingerprint *string        `json:"system_fingerprint"`
	Suggestions       []string       `json:"suggestions"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	LogProbs     *string       `json:"logprobs"`
	FinishReason *string       `json:"finish_reason"`
	Delta        OpenAIDelta   `json:"delta"`
}

type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIDelta struct {
	Content string `json:"content"`
}

type OpenAIImagesGenerationRequest struct {
	OpenAIChatCompletionExtraRequest
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	ResponseFormat string `json:"response_format"`
}

type OpenAIImagesGenerationResponse struct {
	Created     int64                                 `json:"created"`
	DailyLimit  bool                                  `json:"dailyLimit"`
	Data        []*OpenAIImagesGenerationDataResponse `json:"data"`
	Suggestions []string                              `json:"suggestions"`
}

type OpenAIImagesGenerationDataResponse struct {
	URL           string `json:"url"`
	RevisedPrompt string `json:"revised_prompt"`
	B64Json       string `json:"b64_json"`
}

type OpenAIGPT4VImagesReq struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}

// Model represents a model with its properties.
type OpenaiModelResponse struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	//Created time.Time `json:"created"`
	//OwnedBy string    `json:"owned_by"`
}

// ModelList represents a list of models.
type OpenaiModelListResponse struct {
	Object string                `json:"object"`
	Data   []OpenaiModelResponse `json:"data"`
}

func (b *DiscordBot) loadUserAuthTask() {
	for {
		source := rand.NewSource(time.Now().UnixNano())
		randomNumber := rand.New(source).Intn(60) // 生成0到60之间的随机整数

		// 计算距离下一个时间间隔
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location())

		// 如果当前时间已经超过9点，那么等待到第二天的9点
		if now.After(next) {
			next = next.Add(24 * time.Hour)
		}

		delay := next.Sub(now)

		// 等待直到下一个间隔
		time.Sleep(delay + time.Duration(randomNumber)*time.Second)

		logger.DefaultLogger.Info("CDP Scheduled loadUserAuth Task Job Start!")
		b.authorizations = strings.Split(b.authorization, ",")
		logger.DefaultLogger.Info(fmt.Sprintf("UserAuths: %+v", b.authorizations))
		logger.DefaultLogger.Info("CDP Scheduled loadUserAuth Task Job  End!")
	}
}

// messageCreate handles the create messages in Discord.
func (b *DiscordBot) messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// 提前检查参考消息是否为 nil
	if m.ReferencedMessage == nil {
		return
	}

	// 尝试获取 stopChan
	stopChan, exists := b.replyStopChans.Load(m.ReferencedMessage.ID)
	if !exists {
		//channel, myerr := Session.Channel(m.ChannelID)
		// 不存在则直接删除频道
		//if myerr != nil || strings.HasPrefix(channel.Name, "cdp-chat-") {
		//SetChannelDeleteTimer(m.ChannelID, 5*time.Minute)
		return
		//}
	}

	// 如果作者为 nil 或消息来自 bot 本身,则发送停止信号
	if m.Author == nil || m.Author.ID == s.State.User.ID {
		//SetChannelDeleteTimer(m.ChannelID, 5*time.Minute)
		stopChan.(chan ChannelStopChan) <- ChannelStopChan{
			Id: m.ChannelID,
		}
		return
	}

	replyChan, exists := b.repliesChans.Load(m.ReferencedMessage.ID)
	if exists {
		reply := processMessageCreate(m)
		replyChan.(chan ReplyResp) <- reply
	} else {
		logger.DefaultLogger.Debug(m.ReferencedMessage.ID)
		replyOpenAIChan, exists := b.repliesOpenAIChans.Load(m.ReferencedMessage.ID)
		if exists {
			reply := processMessageCreateForOpenAI(m)
			replyOpenAIChan.(chan OpenAIChatCompletionResponse) <- reply
		} else {
			replyOpenAIImageChan, exists := b.repliesOpenAIImageChans.Load(m.ReferencedMessage.ID)
			if exists {
				reply := processMessageCreateForOpenAIImage(m)
				replyOpenAIImageChan.(chan OpenAIImagesGenerationResponse) <- reply
			} else {
				return
			}
		}
	}
	// data: {"id":"chatcmpl-8lho2xvdDFyBdFkRwWAcMpWWAgymJ","object":"chat.completion.chunk","created":1706380498,"model":"gpt-4-turbo-0613","system_fingerprint":null,"choices":[{"index":0,"delta":{"content":"？"},"logprobs":null,"finish_reason":null}]}
	// data :{"id":"1200873365351698694","object":"chat.completion.chunk","created":1706380922,"model":"COZE","choices":[{"index":0,"message":{"role":"assistant","content":"你好！有什么我可以帮您的吗？如果有任"},"logprobs":null,"finish_reason":"","delta":{"content":"吗？如果有任"}}],"usage":{"prompt_tokens":13,"completion_tokens":19,"total_tokens":32},"system_fingerprint":null}

	// 如果消息包含组件或嵌入,则发送停止信号
	if len(m.Message.Components) > 0 {

		var suggestions []string

		actionRow, _ := m.Message.Components[0].(*discordgo.ActionsRow)
		for _, component := range actionRow.Components {
			button := component.(*discordgo.Button)
			suggestions = append(suggestions, button.Label)
		}

		replyOpenAIChan, exists := b.repliesOpenAIChans.Load(m.ReferencedMessage.ID)
		if exists {
			reply := processMessageCreateForOpenAI(m)
			stopStr := "stop"
			reply.Choices[0].FinishReason = &stopStr
			reply.Suggestions = suggestions
			replyOpenAIChan.(chan OpenAIChatCompletionResponse) <- reply
		}

		replyOpenAIImageChan, exists := b.repliesOpenAIImageChans.Load(m.ReferencedMessage.ID)
		if exists {
			reply := processMessageCreateForOpenAIImage(m)
			reply.Suggestions = suggestions
			replyOpenAIImageChan.(chan OpenAIImagesGenerationResponse) <- reply
		}

		stopChan.(chan ChannelStopChan) <- ChannelStopChan{
			Id: m.ChannelID,
		}
	}
}

// messageUpdate handles the updated messages in Discord.
func (b *DiscordBot) messageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	// 提前检查参考消息是否为 nil
	if m.ReferencedMessage == nil {
		return
	}

	// 尝试获取 stopChan
	stopChan, exists := b.replyStopChans.Load(m.ReferencedMessage.ID)
	if !exists {
		channel, err := b.session.Channel(m.ChannelID)
		// 不存在则直接删除频道
		if err != nil || strings.HasPrefix(channel.Name, "cdp-chat-") {
			return
		}
	}

	// 如果作者为 nil 或消息来自 bot 本身,则发送停止信号
	if m.Author == nil || m.Author.ID == s.State.User.ID {
		stopChan.(chan ChannelStopChan) <- ChannelStopChan{
			Id: m.ChannelID,
		}
		return
	}

	replyChan, exists := b.repliesChans.Load(m.ReferencedMessage.ID)
	if exists {
		reply := processMessageUpdate(m)
		replyChan.(chan ReplyResp) <- reply
	} else {
		replyOpenAIChan, exists := b.repliesOpenAIChans.Load(m.ReferencedMessage.ID)
		if exists {
			reply := processMessageUpdateForOpenAI(m)
			replyOpenAIChan.(chan OpenAIChatCompletionResponse) <- reply
		} else {
			replyOpenAIImageChan, exists := b.repliesOpenAIImageChans.Load(m.ReferencedMessage.ID)
			if exists {
				reply := processMessageUpdateForOpenAIImage(m)
				replyOpenAIImageChan.(chan OpenAIImagesGenerationResponse) <- reply
			} else {
				return
			}
		}
	}
	// data: {"id":"chatcmpl-8lho2xvdDFyBdFkRwWAcMpWWAgymJ","object":"chat.completion.chunk","created":1706380498,"model":"gpt-4-turbo-0613","system_fingerprint":null,"choices":[{"index":0,"delta":{"content":"？"},"logprobs":null,"finish_reason":null}]}
	// data :{"id":"1200873365351698694","object":"chat.completion.chunk","created":1706380922,"model":"COZE","choices":[{"index":0,"message":{"role":"assistant","content":"你好！有什么我可以帮您的吗？如果有任"},"logprobs":null,"finish_reason":"","delta":{"content":"吗？如果有任"}}],"usage":{"prompt_tokens":13,"completion_tokens":19,"total_tokens":32},"system_fingerprint":null}

	// 如果消息包含组件或嵌入,则发送停止信号
	if len(m.Message.Components) > 0 {

		var suggestions []string

		actionRow, _ := m.Message.Components[0].(*discordgo.ActionsRow)
		for _, component := range actionRow.Components {
			button := component.(*discordgo.Button)
			suggestions = append(suggestions, button.Label)
		}

		replyOpenAIChan, exists := b.repliesOpenAIChans.Load(m.ReferencedMessage.ID)
		if exists {
			reply := processMessageUpdateForOpenAI(m)
			stopStr := "stop"
			reply.Choices[0].FinishReason = &stopStr
			reply.Suggestions = suggestions
			replyOpenAIChan.(chan OpenAIChatCompletionResponse) <- reply
		}

		replyOpenAIImageChan, exists := b.repliesOpenAIImageChans.Load(m.ReferencedMessage.ID)
		if exists {
			reply := processMessageUpdateForOpenAIImage(m)
			reply.Suggestions = suggestions
			replyOpenAIImageChan.(chan OpenAIImagesGenerationResponse) <- reply
		}

		stopChan.(chan ChannelStopChan) <- ChannelStopChan{
			Id: m.ChannelID,
		}
	}
}

func (b *DiscordBot) SendPlain(message string) ([]string, error) {
	msg, _, err := b.SendRaw(message)
	if err != nil {
		return nil, err
	}
	logger.DefaultLogger.Debug(msg.ID)
	replyChan := make(chan OpenAIChatCompletionResponse)
	b.repliesOpenAIChans.Store(msg.ID, replyChan)
	defer b.repliesOpenAIChans.Delete(msg.ID)

	stopChan := make(chan ChannelStopChan)
	b.replyStopChans.Store(msg.ID, stopChan)
	defer b.replyStopChans.Delete(msg.ID)

	contents := []string{}
	curcontent := ""
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	for {
		select {
		case reply := <-replyChan:
			timer.Reset(60 * time.Second)

			newContent := strings.Replace(reply.Choices[0].Message.Content, curcontent, "", 1)
			if newContent == "" {
				return contents, nil
			}
			reply.Choices[0].Delta.Content = newContent
			curcontent += newContent
			contents = append(contents, newContent)

			if SliceContains(CozeErrorMessages, reply.Choices[0].Message.Content) {
				if SliceContains(CozeDailyLimitErrorMessages, reply.Choices[0].Message.Content) {
					logger.DefaultLogger.Warn(fmt.Sprintf("USER_AUTHORIZATION:%s DAILY LIMIT", b.authorization))
					b.authorizations = FilterSlice(b.authorizations, b.authorization)
				}
			}
			continue
		case <-timer.C:
			return nil, errors.New("未获取到回复")
		case <-stopChan:
			return nil, errors.New("连接中断")
		}
	}
}

func (b *DiscordBot) SendRaw(message string) (*discordgo.Message, string, error) {
	if b.session == nil {
		logger.DefaultLogger.Error("discord session is nil")
		return nil, "", fmt.Errorf("discord session not initialized")
	}

	//var sentMsg *discordgo.Message

	content := fmt.Sprintf("%s \n <@%s>", message, b.botID)

	content = strings.Replace(content, `\u0026`, "&", -1)
	content = strings.Replace(content, `\u003c`, "<", -1)
	content = strings.Replace(content, `\u003e`, ">", -1)

	tokens := CountTokens(content)
	if tokens > 128*1000 {
		logger.DefaultLogger.Error(fmt.Sprintf("prompt已超过限制,请分段发送 [%v] %s", tokens, content))
		return nil, "", fmt.Errorf("prompt已超过限制,请分段发送 [%v]", tokens)
	}

	userAuth, err := RandomElement(b.authorizations)
	if err != nil {
		return nil, "", err
	}

	sendchannelid, err := b.GetSendChannelId()
	if err != nil {
		return nil, "", err
	}

	for i, sendContent := range ReverseSegment(content, 1990) {
		//sentMsg, myerr := Session.ChannelMessageSend(channelID, sendContent)
		//sentMsgId := sentMsg.ID
		// 4.0.0 版本下 用户端发送消息
		sendContent = strings.ReplaceAll(sendContent, "\\n", "\n")
		sentMsgId, err := b.SendMsgByAuthorization(userAuth, sendContent, sendchannelid)
		if err != nil {
			var myErr *DiscordUnauthorizedError
			if errors.As(err, &myErr) {
				// 无效则将此 auth 移除
				b.authorizations = FilterSlice(b.authorizations, userAuth)
				return b.SendRaw(message)
			}
			logger.DefaultLogger.Error(fmt.Sprintf("error sending message: %s", err))
			return nil, "", fmt.Errorf("error sending message")
		}

		time.Sleep(1 * time.Second)

		if i == len(ReverseSegment(content, 1990))-1 {
			return &discordgo.Message{
				ID: sentMsgId,
			}, userAuth, nil
		}
	}
	return &discordgo.Message{}, "", fmt.Errorf("error sending message")
}

func (b *DiscordBot) SendMessageSpec(channelid, bottoken, message string) (*discordgo.Message, string, error) {
	if b.session == nil {
		logger.DefaultLogger.Error("discord session is nil")
		return nil, "", fmt.Errorf("discord session not initialized")
	}

	//var sentMsg *discordgo.Message

	content := fmt.Sprintf("%s \n <@%s>", message, b.botID)

	content = strings.Replace(content, `\u0026`, "&", -1)
	content = strings.Replace(content, `\u003c`, "<", -1)
	content = strings.Replace(content, `\u003e`, ">", -1)

	tokens := CountTokens(content)
	if tokens > 128*1000 {
		logger.DefaultLogger.Error(fmt.Sprintf("prompt已超过限制,请分段发送 [%v] %s", tokens, content))
		return nil, "", fmt.Errorf("prompt已超过限制,请分段发送 [%v]", tokens)
	}

	userAuth, err := RandomElement(b.authorizations)
	if err != nil {
		return nil, "", err
	}

	for i, sendContent := range ReverseSegment(content, 1990) {
		//sentMsg, myerr := Session.ChannelMessageSend(channelID, sendContent)
		//sentMsgId := sentMsg.ID
		// 4.0.0 版本下 用户端发送消息
		sendContent = strings.ReplaceAll(sendContent, "\\n", "\n")
		sentMsgId, err := b.SendMsgByAuthorization(userAuth, sendContent, channelid)
		if err != nil {
			var myErr *DiscordUnauthorizedError
			if errors.As(err, &myErr) {
				// 无效则将此 auth 移除
				b.authorizations = FilterSlice(b.authorizations, userAuth)
				return b.SendRaw(message)
			}
			logger.DefaultLogger.Error(fmt.Sprintf("error sending message: %s", err))
			return nil, "", fmt.Errorf("error sending message")
		}

		time.Sleep(1 * time.Second)

		if i == len(ReverseSegment(content, 1990))-1 {
			return &discordgo.Message{
				ID: sentMsgId,
			}, userAuth, nil
		}
	}
	return &discordgo.Message{}, "", fmt.Errorf("error sending message")
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

func processMessageUpdateForOpenAI(m *discordgo.MessageUpdate) OpenAIChatCompletionResponse {

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

	return OpenAIChatCompletionResponse{
		ID:      m.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "Coze-Model",
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: m.Content,
				},
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func processMessageUpdateForOpenAIImage(m *discordgo.MessageUpdate) OpenAIImagesGenerationResponse {
	var response OpenAIImagesGenerationResponse

	for _, item := range CozeDailyLimitErrorMessages {
		if item == m.Content {
			return OpenAIImagesGenerationResponse{
				Created:    time.Now().Unix(),
				Data:       response.Data,
				DailyLimit: true,
			}
		}
	}

	re := regexp.MustCompile(`]\((https?://\S+)\)`)
	subMatches := re.FindAllStringSubmatch(m.Content, -1)

	if len(subMatches) == 0 && len(m.Embeds) == 0 {
		response.Data = append(response.Data, &OpenAIImagesGenerationDataResponse{
			RevisedPrompt: m.Content,
		})
	}

	for _, match := range subMatches {
		response.Data = append(response.Data, &OpenAIImagesGenerationDataResponse{
			URL:           match[1],
			RevisedPrompt: m.Content,
		})
	}

	if len(m.Embeds) != 0 {
		for _, embed := range m.Embeds {
			if embed.Image != nil && !strings.Contains(m.Content, embed.Image.URL) {
				//	if m.Content != "" {
				//		m.Content += "\n"
				//	}
				response.Data = append(response.Data, &OpenAIImagesGenerationDataResponse{
					URL:           embed.Image.URL,
					RevisedPrompt: m.Content,
				})
			}
		}
	}

	return OpenAIImagesGenerationResponse{
		Created: time.Now().Unix(),
		Data:    response.Data,
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

func processMessageCreateForOpenAI(m *discordgo.MessageCreate) OpenAIChatCompletionResponse {

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

	return OpenAIChatCompletionResponse{
		ID:      m.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "Coze-Model",
		Choices: []OpenAIChoice{
			{
				Index: 0,
				Message: OpenAIMessage{
					Role:    "assistant",
					Content: m.Content,
				},
			},
		},
		Usage: OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func processMessageCreateForOpenAIImage(m *discordgo.MessageCreate) OpenAIImagesGenerationResponse {
	var response OpenAIImagesGenerationResponse

	for _, item := range CozeDailyLimitErrorMessages {
		if item == m.Content {
			return OpenAIImagesGenerationResponse{
				Created:    time.Now().Unix(),
				Data:       response.Data,
				DailyLimit: true,
			}
		}
	}

	re := regexp.MustCompile(`]\((https?://\S+)\)`)
	subMatches := re.FindAllStringSubmatch(m.Content, -1)

	if len(subMatches) == 0 && len(m.Embeds) == 0 {
		response.Data = append(response.Data, &OpenAIImagesGenerationDataResponse{
			RevisedPrompt: m.Content,
		})
	}

	for i, match := range subMatches {
		response.Data = append(response.Data, &OpenAIImagesGenerationDataResponse{
			URL:           match[i],
			RevisedPrompt: m.Content,
		})
	}

	if len(m.Embeds) != 0 {
		for _, embed := range m.Embeds {
			if embed.Image != nil && !strings.Contains(m.Content, embed.Image.URL) {
				//if m.Content != "" {
				//	m.Content += "\n"
				//}
				response.Data = append(response.Data, &OpenAIImagesGenerationDataResponse{
					URL:           embed.Image.URL,
					RevisedPrompt: m.Content,
				})
			}
		}
	}

	return OpenAIImagesGenerationResponse{
		Created: time.Now().Unix(),
		Data:    response.Data,
	}
}

// 用户端发送消息 注意 此为临时解决方案 后续会优化代码
func (b *DiscordBot) SendMsgByAuthorization(userAuth, content, channelId string) (string, error) {
	postUrl := "https://discord.com/api/v9/channels/%s/messages"

	// 构造请求体
	requestBody, err := json.Marshal(map[string]interface{}{
		"content": content,
	})
	if err != nil {
		logger.DefaultLogger.Error(fmt.Sprintf("Error encoding request body:%s", err))
		return "", err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf(postUrl, channelId), bytes.NewBuffer(requestBody))
	if err != nil {
		logger.DefaultLogger.Error(fmt.Sprintf("Error creating request:%s", err))
		return "", err
	}

	// 设置请求头-部分请求头不传没问题，但目前仍有被discord检测异常的风险
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", userAuth)
	req.Header.Set("Origin", "https://discord.com")
	req.Header.Set("Referer", fmt.Sprintf("https://discord.com/channels/%s/%s", b.guildID, channelId))
	if b.userAgent != "" {
		req.Header.Set("User-Agent", b.userAgent)
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/121.0.0.0 Safari/537.36")
	}

	// 发起请求
	client := &http.Client{}
	if b.proxyurl != "" {
		proxyURL, _ := url.Parse(b.proxyurl)
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
		client = &http.Client{
			Transport: transport,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.DefaultLogger.Error(fmt.Sprintf("Error sending request:%s", err))
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应体
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 将响应体转换为字符串
	bodyString := string(bodyBytes)

	// 使用map来解码JSON
	var result map[string]interface{}

	// 解码JSON到map中
	err = json.Unmarshal([]byte(bodyString), &result)
	if err != nil {
		return "", err
	}

	// 类型断言来获取id的值
	id, ok := result["id"].(string)

	if !ok {
		// 401
		if errMessage, ok := result["message"].(string); ok {
			if strings.Contains(errMessage, "401: Unauthorized") ||
				strings.Contains(errMessage, "You need to verify your account in order to perform this action.") {
				logger.DefaultLogger.Warn(fmt.Sprintf("USER_AUTHORIZATION:%s EXPIRED", userAuth))
				return "", &DiscordUnauthorizedError{
					ErrCode: 401,
					Message: "discord 鉴权未通过",
				}
			}
		}
		logger.DefaultLogger.Error(fmt.Sprintf("user_auth:%s result:%s", userAuth, bodyString))
		return "", fmt.Errorf("/api/v9/channels/%s/messages response myerr", channelId)
	} else {
		return id, nil
	}
}

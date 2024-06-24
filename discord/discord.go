package discord

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/h2non/filetype"
	"github.com/pkoukk/tiktoken-go"
	"github.com/sony/sonyflake"
	"github.com/wwqdrh/gokit/logger"
	"golang.org/x/net/proxy"
)

var Version = "v4.5.3" // this hard coding will be replaced automatically when building, no need to manually change

const (
	RequestIdKey = "X-Request-Id"
	OutTime      = "out-time"
)

var ImgGeneratePrompt = "请根据下面的描述生成对应的图片:\\n"

var DefaultOpenaiModelList = []string{
	"gpt-3.5-turbo", "gpt-3.5-turbo-0301", "gpt-3.5-turbo-0613", "gpt-3.5-turbo-1106", "gpt-3.5-turbo-0125",
	"gpt-3.5-turbo-16k", "gpt-3.5-turbo-16k-0613",
	"gpt-3.5-turbo-instruct",
	"gpt-4", "gpt-4-0314", "gpt-4-0613", "gpt-4-1106-preview", "gpt-4-0125-preview",
	"gpt-4-32k", "gpt-4-32k-0314", "gpt-4-32k-0613",
	"gpt-4-turbo-preview", "gpt-4-turbo", "gpt-4-turbo-2024-04-09",
	"gpt-4o", "gpt-4o-2024-05-13",
	"gpt-4-vision-preview",
	"dall-e-3",
}

var CozeErrorMessages = []string{
	"Something wrong occurs, please retry. If the error persists, please contact the support team.",
	"You have exceeded the daily limit for sending messages to the bot. Please try again later.",
	"Some error occurred. Please try again or contact the support team in our communities.",
	"We've detected unusual traffic from your network, so Coze is temporarily unavailable.",
	"There are too many users now. Please try again a bit later.",
	"I'm sorry, but I can't assist with that.",
}

var CozeDailyLimitErrorMessages = []string{
	"You have exceeded the daily limit for sending messages to the bot. Please try again later.",
}

// snowflakeGenerator 单例
var (
	generator *SnowflakeGenerator
	once      sync.Once
)

// SnowflakeGenerator 是雪花ID生成器的封装
type SnowflakeGenerator struct {
	flake *sonyflake.Sonyflake
}

// NextID 生成一个新的雪花ID
func NextID() (string, error) {
	once.Do(func() {
		st := sonyflake.Settings{
			StartTime: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		flake := sonyflake.NewSonyflake(st)
		if flake == nil {
			logger.DefaultLogger.Fatal("sonyflake not created")
		}
		generator = &SnowflakeGenerator{
			flake: flake,
		}
	})
	id, err := generator.flake.NextID()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
}

var (
	Tke *tiktoken.Tiktoken
)

func init() {
	// gpt-4-turbo encoding
	tke, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		logger.DefaultLogger.Fatal(err.Error())
	}
	Tke = tke

}

func CountTokens(text string) int {
	return len(Tke.Encode(text, nil, nil))
}

type ModelNotFoundError struct {
	Message string
	ErrCode int
}

// 实现 error 接口的 Error 方法
func (e *ModelNotFoundError) Error() string {
	return fmt.Sprintf("errCode: %v, message: %v", e.ErrCode, e.Message)
}

// 自定义错误类型
type DiscordUnauthorizedError struct {
	Message string
	ErrCode int
}

// 实现 error 接口的 Error 方法
func (e *DiscordUnauthorizedError) Error() string {
	return fmt.Sprintf("errCode: %v, message: %v", e.ErrCode, e.Message)
}

type BotConfig struct {
	ProxySecret string   `json:"proxySecret"`
	BotId       string   `json:"botId"`
	Model       []string `json:"model"`
	ChannelId   string   `json:"channelId"`
}

// FilterUniqueBotChannel 给定BotConfig切片,筛选出具有不同CozeBotId+ChannelId组合的元素
func FilterUniqueBotChannel(configs []BotConfig) []BotConfig {
	seen := make(map[string]struct{}) // 使用map来跟踪已见的CozeBotId+ChannelId组合
	var uniqueConfigs []BotConfig

	for _, config := range configs {
		combo := config.BotId + "+" + config.ChannelId // 创建组合键
		if _, exists := seen[combo]; !exists {
			seen[combo] = struct{}{} // 标记组合键为已见
			uniqueConfigs = append(uniqueConfigs, config)
		}
	}

	return uniqueConfigs
}

type DiscordBot struct {
	authorization      string
	authorizations     []string
	proxySecret        string
	proxySecrets       []string
	channelAutoDelTime string
	proxyurl           string
	botID              string
	botToken           string
	botAlive           string
	defaultchannel     string
	guildID            string
	userAgent          string
	rateLimit          int
	rateLimitDuration  int64
	maxChannelDelType  string // all oldest

	started                 chan struct{}
	session                 *discordgo.Session
	repliesChans            *sync.Map // map[string]chan ReplyResp
	repliesOpenAIChans      *sync.Map //map[string]chan OpenAIChatCompletionResponse
	repliesOpenAIImageChans *sync.Map //map[string]chan OpenAIImagesGenerationResponse
	replyStopChans          *sync.Map //map[string]chan ChannelStopChan
}

type WithConfig func(*DiscordBot)

func NewDiscordBot(auth string, conf ...WithConfig) *DiscordBot {
	b := &DiscordBot{
		authorization:           auth,
		authorizations:          strings.Split(auth, ","),
		rateLimit:               60,
		rateLimitDuration:       1 * 60,
		started:                 make(chan struct{}),
		repliesChans:            &sync.Map{}, //make(map[string]chan ReplyResp),
		repliesOpenAIChans:      &sync.Map{}, //make(map[string]chan OpenAIChatCompletionResponse),
		repliesOpenAIImageChans: &sync.Map{}, //make(map[string]chan OpenAIImagesGenerationResponse),
		replyStopChans:          &sync.Map{}, //make(map[string]chan ChannelStopChan),
	}
	for _, c := range conf {
		c(b)
	}
	return b
}

func WithRateLimit(limit int) WithConfig {
	return func(db *DiscordBot) {
		db.rateLimit = limit
	}
}

// 服务器id
func WithGuilID(id string) WithConfig {
	return func(db *DiscordBot) {
		db.guildID = id
	}
}

// 要对话的botid
func WithBotID(botid string) WithConfig {
	return func(db *DiscordBot) {
		db.botID = botid
	}
}

// 要对话的bot token
func WithBotToken(token string) WithConfig {
	return func(db *DiscordBot) {
		db.botToken = token
	}
}

// 用于保活的的bot id
func Withdefaultchannel(botid string) WithConfig {
	return func(db *DiscordBot) {
		db.defaultchannel = botid
	}
}
func WithUserAgent(agent string) WithConfig {
	return func(db *DiscordBot) {
		db.userAgent = agent
	}
}

func WithChannelAutoDelTime(deltime string) WithConfig {
	return func(db *DiscordBot) {
		db.channelAutoDelTime = deltime
	}
}

func WithProxyUrl(url string) WithConfig {
	return func(db *DiscordBot) {
		db.proxyurl = url
	}
}

func WithProxySecret(secret string) WithConfig {
	return func(db *DiscordBot) {
		db.proxySecret = secret
		db.proxySecrets = strings.Split(secret, ",")
	}
}

func WithBotAlive(alive string) WithConfig {
	return func(db *DiscordBot) {
		db.botAlive = alive
	}
}
func (b *DiscordBot) Check() {
	if b.proxyurl != "" {
		_, _, err := NewProxyClient(b.proxyurl)
		if err != nil {
			logger.DefaultLogger.Fatal("环境变量 PROXY_URL 设置有误")
		}
	}
	if b.guildID == "" {
		logger.DefaultLogger.Fatal("环境变量 GUILD_ID 未设置")
	}

	if b.botID == "" {
		logger.DefaultLogger.Fatal("环境变量 COZE_BOT_ID 未设置")
	} else if b.session.State.User.ID == b.botID {
		logger.DefaultLogger.Fatal("环境变量 COZE_BOT_ID 不可为当前服务 BOT_TOKEN 关联的 BOT_ID")
	}

	if b.channelAutoDelTime != "" {
		_, err := strconv.Atoi(b.channelAutoDelTime)
		if err != nil {
			logger.DefaultLogger.Fatal("环境变量 CHANNEL_AUTO_DEL_TIME 设置有误")
		}
	}

	logger.DefaultLogger.Info("Environment variable check passed.")
}

func (b *DiscordBot) StartBot(ctx context.Context) {
	var err error
	b.session, err = discordgo.New("Bot " + b.botToken)

	if err != nil {
		logger.DefaultLogger.Fatal("error creating Discord session," + err.Error())
		return
	}

	if b.proxyurl != "" {
		proxyParse, client, err := NewProxyClient(b.proxyurl)
		if err != nil {
			logger.DefaultLogger.Fatal("error creating proxy client," + err.Error())
		}
		b.session.Client = client
		b.session.Dialer.Proxy = http.ProxyURL(proxyParse)
		logger.DefaultLogger.Info("Proxy Set Success!")
	}
	// 注册消息处理函数
	b.session.AddHandler(b.messageCreate)
	b.session.AddHandler(b.messageUpdate)

	// 打开websocket连接并开始监听
	err = b.session.Open()
	if err != nil {
		logger.DefaultLogger.Fatal("error opening connection," + err.Error())
		return
	}
	// 验证docker配置文件
	b.Check()
	logger.DefaultLogger.Info("Bot is now running. Enjoy It.")

	// 每日9点 重新加载userAuth
	go b.loadUserAuthTask()

	if b.botAlive == "1" || b.botAlive == "" {
		// 开启coze保活任务
		go b.stayActiveMessageTask()
	}

	go func() {
		<-ctx.Done()
		if err := b.session.Close(); err != nil {
			logger.DefaultLogger.Fatal("error closing Discord session," + err.Error())
		}
	}()

	// 等待信号
	close(b.started)
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, syscall.SIGTERM)
	<-sc
}

func (b *DiscordBot) ThreadStart(channelId, threadName string, archiveDuration int) (string, error) {
	// 创建新的线程
	th, err := b.session.ThreadStart(channelId, threadName, discordgo.ChannelTypeGuildText, archiveDuration)

	if err != nil {
		logger.DefaultLogger.Error(fmt.Sprintf("创建线程时异常 %s", err.Error()))
		return "", err
	}
	return th.ID, nil
}

func NewProxyClient(proxyUrl string) (proxyParse *url.URL, client *http.Client, err error) {
	proxyParse, err = url.Parse(proxyUrl)
	if err != nil {
		logger.DefaultLogger.Fatal("代理地址设置有误")
	}

	if strings.HasPrefix(proxyParse.Scheme, "http") {
		httpTransport := &http.Transport{
			Proxy: http.ProxyURL(proxyParse),
		}
		return proxyParse, &http.Client{
			Transport: httpTransport,
		}, nil
	} else if strings.HasPrefix(proxyParse.Scheme, "sock") {
		dialer, err := proxy.SOCKS5("tcp", proxyParse.Host, nil, proxy.Direct)
		if err != nil {
			log.Fatal("Error creating dialer, ", err)
		}

		dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}

		// 使用该拨号器创建一个 HTTP 客户端
		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: dialContext,
			},
		}

		return proxyParse, httpClient, nil
	} else {
		return nil, nil, fmt.Errorf("仅支持sock和http代理！")
	}

}

func (b *DiscordBot) stayActiveMessageTask() {
	for {
		source := rand.NewSource(time.Now().UnixNano())
		randomNumber := rand.New(source).Intn(60) // 生成0到60之间的随机整数

		// 计算距离下一个时间间隔
		now := time.Now()
		// 9点05分 为了保证loadUserAuthTask每日任务执行完毕
		next := time.Date(now.Year(), now.Month(), now.Day(), 9, 5, 0, 0, now.Location())

		// 如果当前时间已经超过9点，那么等待到第二天的9点
		if now.After(next) {
			next = next.Add(24 * time.Hour)
		}

		delay := next.Sub(now)

		// 等待直到下一个间隔
		time.Sleep(delay + time.Duration(randomNumber)*time.Second)

		var taskBotConfigs = BotConfigList

		taskBotConfigs = append(taskBotConfigs, BotConfig{
			ChannelId: b.defaultchannel,
			BotId:     b.botID,
		})

		taskBotConfigs = FilterUniqueBotChannel(taskBotConfigs)

		logger.DefaultLogger.Info("CDP Scheduled Task Job Start!")
		var sendChannelList []string
		for _, config := range taskBotConfigs {
			var sendChannelId string
			var err error
			if config.ChannelId == "" {
				nextID, _ := NextID()
				sendChannelId, err = b.CreateChannelWithRetry(b.guildID, fmt.Sprintf("cdp-chat-%s", nextID), 0)
				if err != nil {
					logger.DefaultLogger.Error(err.Error())
					break
				}
				sendChannelList = append(sendChannelList, sendChannelId)
			} else {
				sendChannelId = config.ChannelId
			}
			nextID, err := NextID()
			if err != nil {
				logger.DefaultLogger.Error(fmt.Sprintf("ChannelId{%s} BotId{%s} 活跃机器人任务消息发送异常!雪花Id生成失败!", sendChannelId, config.BotId))
				continue
			}
			_, _, _, err = b.SendMessageSpec(sendChannelId, config.BotId, fmt.Sprintf("【%v】 %s", nextID, "CDP Scheduled Task Job Send Msg Success!"))
			if err != nil {
				logger.DefaultLogger.Error(fmt.Sprintf("ChannelId{%s} BotId{%s} 活跃机器人任务消息发送异常!", sendChannelId, config.BotId))
			} else {
				logger.DefaultLogger.Info(fmt.Sprintf("ChannelId{%s} BotId{%s} 活跃机器人任务消息发送成功!", sendChannelId, config.BotId))
			}
			time.Sleep(5 * time.Second)
		}
		for _, channelId := range sendChannelList {
			b.ChannelDel(channelId)
		}
		logger.DefaultLogger.Info("CDP Scheduled Task Job End!")
	}
}

func getTimeString() string {
	now := time.Now()
	return fmt.Sprintf("%s%d", now.Format("20060102150405"), now.UnixNano()%1e9)
}

func (b *DiscordBot) UploadToDiscordAndGetURL(channelID string, base64Data string) (string, error) {

	// 获取";base64,"后的Base64编码部分
	dataParts := strings.Split(base64Data, ";base64,")
	if len(dataParts) != 2 {
		return "", fmt.Errorf("")
	}
	base64Data = dataParts[1]

	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", err
	}
	// 创建一个新的文件读取器
	file := bytes.NewReader(data)

	kind, err := filetype.Match(data)

	if err != nil {
		return "", fmt.Errorf("无法识别的文件类型")
	}

	// 创建一个新的 MessageSend 结构
	m := &discordgo.MessageSend{
		Files: []*discordgo.File{
			{
				Name:   fmt.Sprintf("file-%s.%s", getTimeString(), kind.Extension),
				Reader: file,
			},
		},
	}

	// 发送消息
	message, err := b.session.ChannelMessageSendComplex(channelID, m)
	if err != nil {
		return "", err
	}

	// 检查消息中是否包含附件,并获取 URL
	if len(message.Attachments) > 0 {
		return message.Attachments[0].URL, nil
	}

	return "", fmt.Errorf("no attachment found in the message")
}

// FilterConfigs 根据proxySecret和channelId过滤BotConfig
func FilterConfigs(configs []BotConfig, secret, gptModel string, channelId *string) []BotConfig {
	var filteredConfigs []BotConfig
	for _, config := range configs {
		matchSecret := secret == "" || config.ProxySecret == secret
		matchGptModel := gptModel == "" || SliceContains(config.Model, gptModel)
		matchChannelId := channelId == nil || *channelId == "" || config.ChannelId == *channelId
		if matchSecret && matchChannelId && matchGptModel {
			filteredConfigs = append(filteredConfigs, config)
		}
	}
	return filteredConfigs
}

func SliceContains(slice []string, str string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

func ReverseSegment(s string, segLen int) []string {
	var result []string
	runeSlice := []rune(s) // 将字符串转换为rune切片，以正确处理多字节字符

	// 从字符串末尾开始切片
	for i := len(runeSlice); i > 0; i -= segLen {
		// 检查是否到达或超过字符串开始
		if i-segLen < 0 {
			// 如果超过，直接从字符串开始到当前位置的所有字符都添加到结果切片中
			result = append([]string{string(runeSlice[0:i])}, result...)
		} else {
			// 否则，从i-segLen到当前位置的子切片添加到结果切片中
			result = append([]string{string(runeSlice[i-segLen : i])}, result...)
		}
	}
	return result
}

// RandomElement 返回给定切片中的随机元素
func RandomElement[T any](slice []T) (T, error) {
	if len(slice) == 0 {
		var zero T
		return zero, fmt.Errorf("empty slice")
	}

	// 随机选择一个索引
	index := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(slice))
	return slice[index], nil
}

func FilterSlice(slice []string, filter string) []string {
	var result []string
	for _, value := range slice {
		if value != filter {
			result = append(result, value)
		}
	}
	return result
}

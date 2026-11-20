package subscription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxPayload caps how much of a subscription is read, in bytes.
const MaxPayload = 8 << 20

// DefaultUserAgent is sent when fetching, because many providers serve
// different (or no) content to unknown clients.
const DefaultUserAgent = "clash-verge/v1.7.7"

// Fetcher downloads subscription payloads. A nil Client uses DefaultClient.
type Fetcher struct {
	Client    *http.Client
	UserAgent string
}

// NewFetcher returns a Fetcher with a 20 second timeout and redirects allowed.
func NewFetcher() *Fetcher {
	return &Fetcher{
		Client: &http.Client{Timeout: 20 * time.Second},
	}
}

// Fetch reads a subscription. http(s) URLs go over the network; file:// URLs
// and bare paths read a local file, which keeps offline testing easy.
func (f *Fetcher) Fetch(ctx context.Context, raw string) ([]byte, error) {
	link := strings.TrimSpace(raw)
	if link == "" {
		return nil, errors.New("请填写订阅链接")
	}

	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("订阅链接格式错误: %w", err)
	}

	if !strings.Contains(link, "://") {
		if _, statErr := os.Stat(link); statErr == nil {
			return readLimited(link)
		}
		return nil, errors.New("链接需要以 http:// 或 https:// 开头")
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return f.fetchHTTP(ctx, link)
	case "file":
		return readLimited(u.Path)
	case "":
		if _, statErr := os.Stat(link); statErr == nil {
			return readLimited(link)
		}
		return nil, errors.New("链接需要以 http:// 或 https:// 开头")
	case "data":
		return nil, errors.New("暂不支持 data: 链接")
	default:
		return nil, fmt.Errorf("不支持的链接协议: %s", u.Scheme)
	}
}

func (f *Fetcher) fetchHTTP(ctx context.Context, link string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	ua := f.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")

	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载订阅失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("下载订阅失败: HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxPayload+1))
	if err != nil {
		return nil, fmt.Errorf("读取订阅失败: %w", err)
	}
	if len(body) > MaxPayload {
		return nil, fmt.Errorf("订阅内容超过 %d MB", MaxPayload>>20)
	}
	return body, nil
}

func readLimited(path string) ([]byte, error) {
	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		return nil, fmt.Errorf("读取本地文件失败: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("这是一个目录，不是订阅文件")
	}
	if info.Size() > MaxPayload {
		return nil, fmt.Errorf("文件超过 %d MB", MaxPayload>>20)
	}
	return os.ReadFile(clean)
}

// SubscriptionURLsOf expands an http(s) URL check hint shared with the UI.
func SubscriptionURLsOf(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "file":
		return true
	}
	return false
}


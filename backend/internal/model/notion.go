package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mmdash/mmdash/backend/internal/settings"
)

const (
	notionAPIBase    = "https://api.notion.com/v1"
	notionAPIVersion = "2026-03-11"
	maxNotionDepth   = 64
	maxNotionPages   = 1000
	maxNotionBlocks  = 20000
)

type NotionExportRequest struct {
	SyncID       string
	ProjectID    string
	SourceID     string
	QuestionID   string
	Mode         string
	RootPageID   string
	TargetPageID string
}

type NotionExport struct {
	SyncID     string             `json:"sync_id"`
	ProjectID  string             `json:"project_id"`
	SourceID   string             `json:"source_id"`
	QuestionID string             `json:"question_id,omitempty"`
	Mode       string             `json:"mode"`
	RootTitle  string             `json:"root_title"`
	Pages      []NotionPageExport `json:"pages"`
}

type NotionPageExport struct {
	PageID       string                   `json:"page_id"`
	ParentPageID string                   `json:"parent_page_id,omitempty"`
	Title        string                   `json:"title"`
	URL          string                   `json:"url"`
	Depth        int                      `json:"depth"`
	Page         map[string]interface{}   `json:"page"`
	Blocks       []map[string]interface{} `json:"blocks"`
}

type NotionClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Version    string
}

func (client NotionClient) Check(ctx context.Context, token, rootPageID string) (string, error) {
	page, err := client.getPage(ctx, token, normalizePageID(rootPageID))
	if err != nil {
		return "", err
	}
	return notionPageTitle(page), nil
}

func (client NotionClient) Export(ctx context.Context, token string, request NotionExportRequest) (NotionExport, error) {
	token = strings.TrimSpace(token)
	request.RootPageID = normalizePageID(request.RootPageID)
	request.TargetPageID = normalizePageID(request.TargetPageID)
	if token == "" || request.SyncID == "" || request.ProjectID == "" || request.SourceID == "" || request.RootPageID == "" || (request.Mode != "discover" && request.Mode != "snapshot") {
		return NotionExport{}, ErrInvalid
	}
	root := request.RootPageID
	if request.Mode == "snapshot" {
		if request.QuestionID == "" || request.TargetPageID == "" {
			return NotionExport{}, ErrInvalid
		}
		root = request.TargetPageID
	}
	export := NotionExport{SyncID: request.SyncID, ProjectID: request.ProjectID, SourceID: request.SourceID, QuestionID: request.QuestionID, Mode: request.Mode, Pages: []NotionPageExport{}}
	blockCount := 0
	visited := map[string]struct{}{}
	var walk func(string, string, int) error
	walk = func(pageID, parentPageID string, depth int) error {
		if depth > maxNotionDepth || len(export.Pages) >= maxNotionPages {
			return ErrSyncUnavailable
		}
		pageID = normalizePageID(pageID)
		if pageID == "" {
			return ErrInvalid
		}
		if _, exists := visited[pageID]; exists {
			return nil
		}
		visited[pageID] = struct{}{}
		page, err := client.getPage(ctx, token, pageID)
		if err != nil {
			return err
		}
		blocks, err := client.getBlockTree(ctx, token, pageID, 0, &blockCount)
		if err != nil {
			return err
		}
		pageURL, _ := page["url"].(string)
		entry := NotionPageExport{PageID: pageID, ParentPageID: parentPageID, Title: notionPageTitle(page), URL: pageURL, Depth: depth, Page: page, Blocks: blocks}
		if depth == 0 {
			export.RootTitle = entry.Title
		}
		if request.Mode == "snapshot" || depth > 0 {
			export.Pages = append(export.Pages, entry)
		}
		for _, childID := range collectChildPageIDs(blocks) {
			if err := walk(childID, pageID, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, "", 0); err != nil {
		return NotionExport{}, err
	}
	return export, nil
}

func (client NotionClient) getPage(ctx context.Context, token, pageID string) (map[string]interface{}, error) {
	var result map[string]interface{}
	if err := client.requestJSON(ctx, token, http.MethodGet, "/pages/"+url.PathEscape(pageID), nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (client NotionClient) getBlockTree(ctx context.Context, token, blockID string, depth int, count *int) ([]map[string]interface{}, error) {
	if depth > maxNotionDepth {
		return nil, ErrSyncUnavailable
	}
	result := []map[string]interface{}{}
	cursor := ""
	for {
		path := "/blocks/" + url.PathEscape(blockID) + "/children?page_size=100"
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}
		var response struct {
			Results    []map[string]interface{} `json:"results"`
			HasMore    bool                     `json:"has_more"`
			NextCursor *string                  `json:"next_cursor"`
		}
		if err := client.requestJSON(ctx, token, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		for _, block := range response.Results {
			(*count)++
			if *count > maxNotionBlocks {
				return nil, ErrSyncUnavailable
			}
			blockType, _ := block["type"].(string)
			hasChildren, _ := block["has_children"].(bool)
			if sourceID := syncedSourceBlockID(block); blockType == "synced_block" && sourceID != "" {
				children, err := client.getBlockTree(ctx, token, sourceID, depth+1, count)
				if err != nil {
					return nil, err
				}
				block["children"] = children
			} else if hasChildren && blockType != "child_page" && blockType != "child_database" {
				id, _ := block["id"].(string)
				children, err := client.getBlockTree(ctx, token, id, depth+1, count)
				if err != nil {
					return nil, err
				}
				block["children"] = children
			}
			result = append(result, block)
		}
		if !response.HasMore {
			break
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			return nil, ErrSyncUnavailable
		}
		cursor = *response.NextCursor
	}
	return result, nil
}

func syncedSourceBlockID(block map[string]interface{}) string {
	data, _ := block["synced_block"].(map[string]interface{})
	from, _ := data["synced_from"].(map[string]interface{})
	if fromType, _ := from["type"].(string); fromType != "block_id" {
		return ""
	}
	value, _ := from["block_id"].(string)
	return normalizePageID(value)
}

func (client NotionClient) requestJSON(ctx context.Context, token, method, path string, body interface{}, target interface{}) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	base := strings.TrimRight(client.BaseURL, "/")
	if base == "" {
		base = notionAPIBase
	}
	version := strings.TrimSpace(client.Version)
	if version == "" {
		version = notionAPIVersion
	}
	httpClient := client.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	for attempt := 0; attempt < 4; attempt++ {
		request, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Notion-Version", version)
		request.Header.Set("Accept", "application/json")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := httpClient.Do(request)
		if err != nil {
			return fmt.Errorf("request Notion: %w", err)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
		closeErr := response.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Notion response: %w", readErr)
		}
		if len(responseBody) > 8*1024*1024 {
			return fmt.Errorf("%w: Notion response is too large", ErrSyncUnavailable)
		}
		if closeErr != nil {
			return fmt.Errorf("close Notion response: %w", closeErr)
		}
		if response.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := time.Second
			if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil && seconds > 0 && seconds <= 30 {
				delay = time.Duration(seconds) * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			var notionError struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(responseBody, &notionError)
			switch response.StatusCode {
			case http.StatusUnauthorized:
				return fmt.Errorf("%w: Notion access token was rejected (%s)", ErrNotionUnauthorized, notionError.Code)
			case http.StatusForbidden, http.StatusNotFound:
				return fmt.Errorf("%w: Notion access denied (%s)", ErrNotConfigured, notionError.Code)
			default:
				return fmt.Errorf("%w: Notion returned HTTP %d (%s)", ErrSyncUnavailable, response.StatusCode, notionError.Code)
			}
		}
		if err := json.Unmarshal(responseBody, target); err != nil {
			return fmt.Errorf("decode Notion response: %w", err)
		}
		return nil
	}
	return ErrSyncUnavailable
}

func notionPageTitle(page map[string]interface{}) string {
	properties, _ := page["properties"].(map[string]interface{})
	for _, raw := range properties {
		property, _ := raw.(map[string]interface{})
		if property["type"] != "title" {
			continue
		}
		items, _ := property["title"].([]interface{})
		var value strings.Builder
		for _, itemRaw := range items {
			item, _ := itemRaw.(map[string]interface{})
			plain, _ := item["plain_text"].(string)
			value.WriteString(plain)
		}
		if title := strings.TrimSpace(value.String()); title != "" {
			return title
		}
	}
	return "Untitled"
}

func collectChildPageIDs(blocks []map[string]interface{}) []string {
	result := []string{}
	for _, block := range blocks {
		if block["type"] == "child_page" {
			id, _ := block["id"].(string)
			if id = normalizePageID(id); id != "" {
				result = append(result, id)
			}
		}
		children, _ := block["children"].([]map[string]interface{})
		result = append(result, collectChildPageIDs(children)...)
		if generic, ok := block["children"].([]interface{}); ok {
			converted := make([]map[string]interface{}, 0, len(generic))
			for _, child := range generic {
				if value, ok := child.(map[string]interface{}); ok {
					converted = append(converted, value)
				}
			}
			result = append(result, collectChildPageIDs(converted)...)
		}
	}
	return result
}

func SettingDefinition(exporter NotionExporter) settings.TypeDefinition {
	return settings.TypeDefinition{
		Key: SettingTypeNotion, Owner: "model", Title: "Notion 模型来源", Order: 60,
		Description: "通过 mmdash 公共集成授权单一 Notion 根页面并配置自动同步策略。",
		Scopes:      []settings.Scope{settings.ScopeProject},
		Fields: []settings.FieldDefinition{
			{Key: "access_token", Kind: settings.FieldSecret, Label: "OAuth access token", Description: "由 Notion OAuth 回调写入；浏览器不可读取。"},
			{Key: "refresh_token", Kind: settings.FieldSecret, Label: "OAuth refresh token", Description: "由 Notion OAuth 回调写入；浏览器不可读取。"},
			{Key: "integration_token", Kind: settings.FieldSecret, Label: "Legacy Integration Token", Description: "仅用于迁移升级前的现有连接，Web 不再接受新增。"},
			{Key: "root_page_url", Kind: settings.FieldURL, Label: "根页面 URL", Required: true},
			{Key: "auto_sync_enabled", Kind: settings.FieldBoolean, Label: "启用自动同步", Required: true},
			{Key: "auto_sync_interval_seconds", Kind: settings.FieldNumber, Label: "同步间隔（秒）", Required: true, Description: "60–86400 秒，默认 300 秒。"},
			{Key: "oauth_bot_id", Kind: settings.FieldString, Label: "Notion bot ID"},
			{Key: "oauth_workspace_id", Kind: settings.FieldString, Label: "Notion workspace ID"},
			{Key: "oauth_workspace_name", Kind: settings.FieldString, Label: "Notion workspace name"},
			{Key: "oauth_workspace_icon", Kind: settings.FieldURL, Label: "Notion workspace icon"},
		},
		Tester: NotionSettingTester{Exporter: exporter},
	}
}

type NotionSettingTester struct{ Exporter NotionExporter }

func (tester NotionSettingTester) Test(ctx context.Context, setting settings.ResolvedSetting) ([]settings.ConnectionCheck, error) {
	if tester.Exporter == nil {
		return nil, errors.New("Notion adapter unavailable")
	}
	token := settingString(setting, "access_token")
	if token == "" {
		token = settingString(setting, "integration_token")
	}
	rootURL := settingString(setting, "root_page_url")
	pageID, _, err := parseNotionPageURL(rootURL)
	if err != nil {
		return []settings.ConnectionCheck{{Name: "root_page", Status: "failed", Message: "Notion 根页面 URL 无效"}}, nil
	}
	title, err := tester.Exporter.Check(ctx, token, pageID)
	if err != nil {
		return []settings.ConnectionCheck{{Name: "authentication", Status: "failed", Message: "无法读取已绑定的 Notion 根页面"}}, nil
	}
	return []settings.ConnectionCheck{{Name: "authentication", Status: "passed"}, {Name: "root_page", Status: "passed", Message: title}}, nil
}

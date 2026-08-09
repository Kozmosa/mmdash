package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	rootPageID  = "00000000-0000-4000-8000-000000000001"
	childPageID = "00000000-0000-4000-8000-000000000002"
	grandPageID = "00000000-0000-4000-8000-000000000003"
)

func TestNotionExportRecursivelyDiscoversChildPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret-token" || request.Header.Get("Notion-Version") != notionAPIVersion {
			t.Fatalf("missing Notion headers")
		}
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/pages/" + rootPageID:
			fmt.Fprintf(response, `{"url":"https://www.notion.so/root","properties":{"Name":{"type":"title","title":[{"plain_text":"模型根页面"}]}}}`)
		case "/blocks/" + rootPageID + "/children":
			fmt.Fprintf(response, `{"results":[{"id":"%s","type":"child_page","has_children":true,"child_page":{"title":"Q1"}}],"has_more":false,"next_cursor":null}`, childPageID)
		case "/pages/" + childPageID:
			fmt.Fprintf(response, `{"url":"https://www.notion.so/q1","properties":{"Name":{"type":"title","title":[{"plain_text":"Q1 模型"}]}}}`)
		case "/blocks/" + childPageID + "/children":
			fmt.Fprintf(response, `{"results":[{"id":"%s","type":"child_page","has_children":true,"child_page":{"title":"推导"}}],"has_more":false,"next_cursor":null}`, grandPageID)
		case "/pages/" + grandPageID:
			fmt.Fprintf(response, `{"url":"https://www.notion.so/q1-derivation","properties":{"Name":{"type":"title","title":[{"plain_text":"Q1 推导"}]}}}`)
		case "/blocks/" + grandPageID + "/children":
			fmt.Fprint(response, `{"results":[],"has_more":false,"next_cursor":null}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := NotionClient{BaseURL: server.URL, HTTPClient: server.Client()}

	exported, err := client.Export(context.Background(), "secret-token", NotionExportRequest{SyncID: "sync", ProjectID: "project", SourceID: "source", Mode: "discover", RootPageID: rootPageID})
	if err != nil {
		t.Fatal(err)
	}
	if exported.RootTitle != "模型根页面" || len(exported.Pages) != 2 || exported.Pages[0].PageID != childPageID || exported.Pages[0].ParentPageID != rootPageID || exported.Pages[0].Depth != 1 || exported.Pages[1].PageID != grandPageID || exported.Pages[1].ParentPageID != childPageID || exported.Pages[1].Depth != 2 {
		t.Fatalf("unexpected recursive export: %#v", exported)
	}
}

func TestNotionErrorsNeverExposeToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, `{"code":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	client := NotionClient{BaseURL: server.URL, HTTPClient: server.Client()}
	_, err := client.Check(context.Background(), "very-secret-token", rootPageID)
	if err == nil || strings.Contains(err.Error(), "very-secret-token") {
		t.Fatalf("expected safe Notion error, got %v", err)
	}
}

func TestNotionExportResolvesReferencedSyncedBlockChildren(t *testing.T) {
	sourceBlockID := "00000000-0000-4000-8000-000000000004"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/pages/" + rootPageID:
			fmt.Fprint(response, `{"url":"https://www.notion.so/root","properties":{"Name":{"type":"title","title":[{"plain_text":"Q1"}]}}}`)
		case "/blocks/" + rootPageID + "/children":
			fmt.Fprintf(response, `{"results":[{"id":"reference","type":"synced_block","has_children":true,"synced_block":{"synced_from":{"type":"block_id","block_id":"%s"}}}],"has_more":false,"next_cursor":null}`, sourceBlockID)
		case "/blocks/" + sourceBlockID + "/children":
			fmt.Fprint(response, `{"results":[{"id":"paragraph","type":"paragraph","has_children":false,"paragraph":{"rich_text":[{"plain_text":"同步内容"}]}}],"has_more":false,"next_cursor":null}`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client := NotionClient{BaseURL: server.URL, HTTPClient: server.Client()}

	exported, err := client.Export(context.Background(), "secret-token", NotionExportRequest{SyncID: "sync", ProjectID: "project", SourceID: "source", QuestionID: "question", Mode: "snapshot", RootPageID: rootPageID, TargetPageID: rootPageID})
	if err != nil {
		t.Fatal(err)
	}
	children, _ := exported.Pages[0].Blocks[0]["children"].([]map[string]interface{})
	if len(children) != 1 || children[0]["id"] != "paragraph" {
		t.Fatalf("referenced synced block children = %#v", children)
	}
}

func TestParseNotionPageURLAcceptsCurrentAppLinks(t *testing.T) {
	pageID, normalized, err := parseNotionPageURL("https://app.notion.com/p/nyaku/1-3a4df00a545d801cae41e79dc52fbb51")
	if err != nil {
		t.Fatal(err)
	}
	if pageID != "3a4df00a-545d-801c-ae41-e79dc52fbb51" {
		t.Fatalf("unexpected page id %q", pageID)
	}
	if normalized != "https://app.notion.com/p/nyaku/1-3a4df00a545d801cae41e79dc52fbb51" {
		t.Fatalf("unexpected normalized URL %q", normalized)
	}
}

func TestParseNotionPageURLAcceptsPublishedSiteLinks(t *testing.T) {
	pageID, _, err := parseNotionPageURL("https://nyaku.notion.site/3a4df00a545d801cae41e79dc52fbb51?source=copy_link")
	if err != nil || pageID != "3a4df00a-545d-801c-ae41-e79dc52fbb51" {
		t.Fatalf("published URL = %q, %v", pageID, err)
	}
}

func TestParseNotionPageURLRejectsLookalikeHostsAndCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://app.notion.com.evil.example/p/nyaku/3a4df00a545d801cae41e79dc52fbb51",
		"https://notion.com@evil.example/3a4df00a545d801cae41e79dc52fbb51",
		"https://user@app.notion.com/3a4df00a545d801cae41e79dc52fbb51",
		"https://app.notion.com:8443/3a4df00a545d801cae41e79dc52fbb51",
	} {
		if _, _, err := parseNotionPageURL(raw); err == nil {
			t.Fatalf("expected URL to be rejected: %s", raw)
		}
	}
}

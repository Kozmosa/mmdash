package main

import "testing"

func TestAnswerPromptOnlyReturnsReviewedCredentialFields(t *testing.T) {
	username, ok := answerPrompt("Username for 'https://github.com':", "", "secret")
	if !ok || username != "x-access-token" {
		t.Fatalf("unexpected username answer: %q, %v", username, ok)
	}
	token, ok := answerPrompt("Password for 'https://github.com':", "user", "secret")
	if !ok || token != "secret" {
		t.Fatalf("unexpected token answer: %q, %v", token, ok)
	}
	if answer, accepted := answerPrompt("Unknown prompt", "user", "secret"); accepted || answer != "" {
		t.Fatal("unknown prompts must not receive credentials")
	}
	if answer, accepted := answerPrompt("Password:", "user", ""); accepted || answer != "" {
		t.Fatal("empty tokens must not be returned")
	}
}

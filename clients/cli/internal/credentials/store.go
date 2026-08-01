package credentials

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("credentials not found")

type Credential struct {
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token"`
	SessionID    string    `json:"session_id"`
}

type Store interface {
	Delete(profile string) error
	Get(profile string) (Credential, error)
	Set(profile string, credential Credential) error
}

func encode(credential Credential) (string, error) {
	bytes, err := json.Marshal(credential)
	return string(bytes), err
}

func decode(value string) (Credential, error) {
	var credential Credential
	err := json.Unmarshal([]byte(value), &credential)
	return credential, err
}

type MemoryStore struct {
	mu     sync.Mutex
	values map[string]Credential
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{values: map[string]Credential{}} }

func (store *MemoryStore) Get(profile string) (Credential, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.values[profile]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return value, nil
}

func (store *MemoryStore) Set(profile string, credential Credential) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.values[profile] = credential
	return nil
}

func (store *MemoryStore) Delete(profile string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.values, profile)
	return nil
}

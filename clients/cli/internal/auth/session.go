package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/mmdash/mmdash/clients/cli/internal/api"
	"github.com/mmdash/mmdash/clients/cli/internal/apperror"
	"github.com/mmdash/mmdash/clients/cli/internal/credentials"
)

type Session struct {
	API     *api.Client
	Profile string
	Store   credentials.Store
	Now     func() time.Time
}

func New(client *api.Client, store credentials.Store, serverURL string) *Session {
	sum := sha256.Sum256([]byte(serverURL))
	return &Session{API: client, Profile: hex.EncodeToString(sum[:12]), Store: store, Now: time.Now}
}

func (session *Session) Save(result api.LoginResult) error {
	return session.Store.Set(session.Profile, credentials.Credential{AccessToken: result.AccessToken, ExpiresAt: result.ExpiresAt, RefreshToken: result.RefreshToken, SessionID: result.SessionID})
}

func (session *Session) AccessToken(ctx context.Context, forceRefresh bool) (string, error) {
	credential, err := session.Store.Get(session.Profile)
	if errors.Is(err, credentials.ErrNotFound) {
		return "", apperror.New("AUTH_REQUIRED", "Run 'mmdash login' first", 3)
	}
	if err != nil {
		return "", apperror.Wrap("CREDENTIAL_STORE_ERROR", "Cannot read the system credential store", 3, err)
	}
	if !forceRefresh && credential.AccessToken != "" && credential.ExpiresAt.After(session.Now().Add(5*time.Minute)) {
		return credential.AccessToken, nil
	}
	result, err := session.API.Refresh(ctx, credential.RefreshToken)
	if err != nil {
		return "", translate(err)
	}
	if err := session.Save(result); err != nil {
		return "", apperror.Wrap("CREDENTIAL_STORE_ERROR", "Cannot update the system credential store", 3, err)
	}
	return result.AccessToken, nil
}

func (session *Session) Delete() error {
	if err := session.Store.Delete(session.Profile); err != nil {
		return apperror.Wrap("CREDENTIAL_STORE_ERROR", "Cannot remove the CLI credential", 3, err)
	}
	return nil
}

func translate(err error) error {
	var remote *api.Error
	if errors.As(err, &remote) {
		exitCode := 1
		if remote.Status == 401 {
			exitCode = 3
		}
		return &apperror.Error{Code: remote.Code, ExitCode: exitCode, Message: remote.Message, RequestID: remote.RequestID, Retryable: remote.Status >= 500, Cause: err}
	}
	return err
}

func Translate(err error) error { return translate(err) }

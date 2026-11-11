package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type API struct {
	Auth Auth
	HTTP *http.Client
}

func (a API) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	access, err := a.Auth.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Auth.Config.APIURL, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := a.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("authentication rejected; run polyagent login: %w", responseError(response))
		}
		return nil, responseError(response)
	}
	return response, nil
}

func (a API) JSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	response, err := a.request(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return json.RawMessage("null"), nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid JSON from %s", path)
	}
	return data, nil
}

func (a API) Stream(ctx context.Context, sessionID, message string) (*http.Response, error) {
	return a.request(ctx, http.MethodPost, "/api/sessions/"+url.PathEscape(sessionID)+"/messages", map[string]string{"content": message})
}

func (a Auth) UpdateUser(ctx context.Context, fields map[string]string) error {
	access, err := a.AccessToken(ctx)
	if err != nil {
		return err
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(a.Config.SupabaseURL, "/")+"/auth/v1/user", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("apikey", a.Config.AnonKey)
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	response, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return responseError(response)
	}
	return nil
}

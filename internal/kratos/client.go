// Package kratos is a minimal client for the pieces of Ory Kratos's public
// and admin APIs Crewtopia needs: reading the current session, fetching a
// self-service flow to render its form, and (for the super-admin invite
// flow) creating identities and recovery links via the admin API.
//
// It deliberately doesn't use Ory's official SDK — the surface we touch is
// small enough that a hand-rolled client keeps the dependency list at zero
// extra packages for talking to Kratos.
package kratos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PublicProxyPrefix is the path Kratos's public API is reachable at on our
// own origin, matching serve.public.base_url in kratos.yml. Oathkeeper
// proxies this prefix straight through to Kratos (see
// deploy/oathkeeper/access-rules.yml, rule "kratos-public-api"); the app
// only needs the constant to build redirect URLs that kick off a flow.
const PublicProxyPrefix = "/.ory/kratos/public"

type Client struct {
	// PublicURL is Kratos's public API, reachable server-to-server (e.g.
	// http://localhost:4433 in dev, http://kratos:4433 inside compose).
	PublicURL string
	// AdminURL is Kratos's admin API (identity CRUD, recovery links) —
	// never exposed to the browser.
	AdminURL string

	httpClient *http.Client
}

func NewClient(publicURL, adminURL string) *Client {
	return &Client{
		PublicURL:  publicURL,
		AdminURL:   adminURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// WhoAmISession is the subset of Kratos's whoami response Crewtopia cares
// about. Role lives in MetadataPublic, not Traits — see the comment on
// CreateIdentity for why.
type WhoAmISession struct {
	Identity struct {
		ID             string `json:"id"`
		MetadataPublic struct {
			Role string `json:"role"`
		} `json:"metadata_public"`
		Traits struct {
			Email string `json:"email"`
		} `json:"traits"`
	} `json:"identity"`
}

// WhoAmI checks whether cookieHeader carries a valid Kratos session. This is
// a deliberate, narrow exception to Phase 7's "the app trusts Oathkeeper's
// headers, it doesn't talk to Kratos directly" rule: it exists only so the
// public roster page can decide whether to show the full nav or the
// logged-out one. It is never used to gate access to anything — every route
// that actually needs to be protected still goes through Oathkeeper's
// cookie_session check and the app's requireTrustedHeaders middleware.
func (c *Client) WhoAmI(ctx context.Context, cookieHeader string) (*WhoAmISession, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.PublicURL+"/sessions/whoami", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whoami: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("whoami: no active session")
	}

	var sess WhoAmISession
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		return nil, fmt.Errorf("decode whoami response: %w", err)
	}
	return &sess, nil
}

// FlowNode is one form field (or hidden value, or submit button) in a
// self-service flow's UI, as Kratos represents it.
type FlowNode struct {
	Group      string `json:"group"`
	Attributes struct {
		NodeType string `json:"node_type"` // "input", "text", "img", ...
		Name     string `json:"name"`
		Type     string `json:"type"` // "hidden", "email", "password", "submit", ...
		Value    any    `json:"value"`
		Required bool   `json:"required"`
		Disabled bool   `json:"disabled"`
	} `json:"attributes"`
	Meta struct {
		Label struct {
			Text string `json:"text"`
		} `json:"label"`
	} `json:"meta"`
	Messages []struct {
		Text string `json:"text"`
	} `json:"messages"`
}

// Label returns the human-facing label Kratos suggests for this node,
// falling back to the field name if Kratos didn't send one.
func (n FlowNode) Label() string {
	if n.Meta.Label.Text != "" {
		return n.Meta.Label.Text
	}
	return n.Attributes.Name
}

// Flow is the shared shape of Kratos's login/recovery/settings self-service
// flows: a form description (action, method, nodes) plus top-level messages
// (e.g. "invalid credentials").
type Flow struct {
	ID string `json:"id"`
	UI struct {
		Action   string `json:"action"`
		Method   string `json:"method"`
		Nodes    []FlowNode
		Messages []struct {
			Text string `json:"text"`
		} `json:"messages"`
	} `json:"ui"`
}

// GetFlow fetches an in-progress flow (kind is "login", "recovery", or
// "settings") by ID, forwarding the browser's cookies so Kratos can match
// the flow to its CSRF cookie.
func (c *Client) GetFlow(ctx context.Context, kind, flowID, cookieHeader string) (*Flow, error) {
	url := fmt.Sprintf("%s/self-service/%s/flows?id=%s", c.PublicURL, kind, flowID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s flow: %w", kind, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get %s flow: status %d: %s", kind, resp.StatusCode, body)
	}

	var flow Flow
	if err := json.NewDecoder(resp.Body).Decode(&flow); err != nil {
		return nil, fmt.Errorf("decode %s flow: %w", kind, err)
	}
	return &flow, nil
}

func (c *Client) adminJSON(ctx context.Context, method, path string, reqBody, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.AdminURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("admin %s %s: status %d: %s", method, path, resp.StatusCode, respBytes)
	}
	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("decode admin %s %s response: %w", method, path, err)
		}
	}
	return nil
}

// CreateIdentity creates a login with the given email/role and no
// credentials — the account is unusable until the recovery link below is
// redeemed and a password is set.
//
// role is set via metadata_public, not traits, and identity.schema.json has
// no "role" trait at all. That's load-bearing, not a style choice: a JSON
// Schema "readOnly" hint on a trait does NOT stop Kratos from persisting a
// self-submitted change to it — confirmed by testing, a "staff" identity
// could POST traits.role=super_admin to its own settings flow and it took
// effect. metadata_public is never part of any self-service flow (only the
// admin API can set it), so it's the only way to guarantee a staff account
// can't self-escalate.
func (c *Client) CreateIdentity(ctx context.Context, email, role string) (identityID string, err error) {
	reqBody := map[string]any{
		"schema_id": "default",
		"traits": map[string]any{
			"email": email,
		},
		"metadata_public": map[string]any{
			"role": role,
		},
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := c.adminJSON(ctx, http.MethodPost, "/admin/identities", reqBody, &resp); err != nil {
		return "", err
	}
	return resp.ID, nil
}

// CreateRecoveryLink generates a one-time link that lets identityID set a
// password, without sending any email — the caller (the super-admin invite
// handler) is responsible for getting it to the new user.
func (c *Client) CreateRecoveryLink(ctx context.Context, identityID string) (link string, expiresAt string, err error) {
	reqBody := map[string]any{
		"identity_id": identityID,
		"expires_in":  "24h",
	}
	var resp struct {
		RecoveryLink string `json:"recovery_link"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := c.adminJSON(ctx, http.MethodPost, "/admin/recovery/link", reqBody, &resp); err != nil {
		return "", "", err
	}
	return resp.RecoveryLink, resp.ExpiresAt, nil
}

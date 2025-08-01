package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type UserliService interface {
	GetAliases(email string) ([]string, error)
	GetDomain(domain string) (bool, error)
	GetMailbox(email string) (bool, error)
	GetSenders(email string) ([]string, error)
}

type Userli struct {
	token   string
	baseURL string

	mu     sync.RWMutex // Protects Client field
	Client *http.Client
}

// Option defines a functional option for configuring Userli
type Option func(*Userli)

// WithClient sets a custom HTTP client (thread-safe)
func WithClient(client *http.Client) Option {
	return func(u *Userli) {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.Client = client
	}
}

// WithTransport sets a custom transport (creates a new client with this transport, thread-safe)
func WithTransport(transport *http.Transport) Option {
	return func(u *Userli) {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.Client = &http.Client{
			Transport: transport,
			Timeout:   time.Second * 10,
		}
	}
}

// WithTimeout sets a custom timeout (creates a new client with the specified timeout, thread-safe)
func WithTimeout(timeout time.Duration) Option {
	return func(u *Userli) {
		u.mu.Lock()
		defer u.mu.Unlock()

		// Always create a new client to avoid race conditions
		// Copy transport from existing client if available
		var transport http.RoundTripper
		if u.Client != nil && u.Client.Transport != nil {
			transport = u.Client.Transport
		} else {
			// Use default optimized transport
			transport = &http.Transport{
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   30,
				MaxConnsPerHost:       100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				DisableKeepAlives:     false,
			}
		}

		u.Client = &http.Client{
			Transport: transport,
			Timeout:   timeout,
		}
	}
}

func NewUserli(token, baseURL string, opts ...Option) *Userli {
	u := &Userli{
		token:   token,
		baseURL: baseURL,
	}

	// Apply options
	for _, opt := range opts {
		opt(u)
	}

	// Set default client if none was provided
	if u.Client == nil {
		transport := &http.Transport{
			MaxIdleConns:          100,              // Maximum idle connections across all hosts
			MaxIdleConnsPerHost:   30,               // Maximum idle connections per host
			MaxConnsPerHost:       100,              // Maximum connections per host
			IdleConnTimeout:       90 * time.Second, // How long idle connections stay open
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     false, // Enable keep-alive
		}

		u.Client = &http.Client{
			Transport: transport,
			Timeout:   time.Second * 10,
		}
	}

	return u
}

func (u *Userli) GetAliases(email string) ([]string, error) {
	if !strings.Contains(email, "@") {
		return []string{}, nil
	}

	resp, err := u.call(fmt.Sprintf("%s/api/postfix/alias/%s", u.baseURL, email))
	if err != nil {
		return []string{}, err
	}

	var aliases []string
	err = json.NewDecoder(resp.Body).Decode(&aliases)
	if err != nil {
		return []string{}, err
	}

	return aliases, nil
}

func (u *Userli) GetDomain(domain string) (bool, error) {
	resp, err := u.call(fmt.Sprintf("%s/api/postfix/domain/%s", u.baseURL, domain))
	if err != nil {
		return false, err
	}

	var result bool
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return false, err
	}

	return result, nil
}

func (u *Userli) GetMailbox(email string) (bool, error) {
	if !strings.Contains(email, "@") {
		return false, nil
	}

	resp, err := u.call(fmt.Sprintf("%s/api/postfix/mailbox/%s", u.baseURL, email))
	if err != nil {
		return false, err
	}

	var result bool
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return false, err
	}

	return result, nil
}

func (u *Userli) GetSenders(email string) ([]string, error) {
	if !strings.Contains(email, "@") {
		return []string{}, nil
	}

	resp, err := u.call(fmt.Sprintf("%s/api/postfix/senders/%s", u.baseURL, email))
	if err != nil {
		return []string{}, err
	}

	var senders []string
	err = json.NewDecoder(resp.Body).Decode(&senders)
	if err != nil {
		return []string{}, err
	}

	return senders, nil
}

func (u *Userli) call(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", u.token))

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "userli-postfix-adapter")

	// Get client with read lock for thread safety
	u.mu.RLock()
	client := u.Client
	u.mu.RUnlock()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// validLocalPartRegex validates that the local part only contains allowed characters: a-z, 0-9, -, _, .
var validLocalPartRegex = regexp.MustCompile(`^[a-z0-9\-_.]*$`)

type UserliService interface {
	GetAliases(ctx context.Context, email string) ([]string, error)
	GetDomain(ctx context.Context, domain string) (bool, error)
	GetMailbox(ctx context.Context, email string) (bool, error)
	GetSenders(ctx context.Context, email string) ([]string, error)
	GetQuota(ctx context.Context, email string) (*Quota, error)
}

// Quota represents the sending quota limits for a user
type Quota struct {
	PerHour int `json:"per_hour"`
	PerDay  int `json:"per_day"`
}

type Userli struct {
	token     string
	baseURL   string
	delimiter string

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

func WithDelimiter(delimiter string) Option {
	return func(u *Userli) {
		u.mu.Lock()
		defer u.mu.Unlock()
		u.delimiter = delimiter
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

func (u *Userli) GetAliases(ctx context.Context, email string) ([]string, error) {
	sanitizedEmail, err := u.sanitizeEmail(email)
	if err != nil {
		logger.Info("unable to process the alias", zap.String("email", email), zap.Error(err))
		return []string{}, nil
	}

	resp, err := u.call(ctx, fmt.Sprintf("%s/api/postfix/alias/%s", u.baseURL, sanitizedEmail))
	if err != nil {
		return []string{}, err
	}
	defer resp.Body.Close()

	var aliases []string
	err = json.NewDecoder(resp.Body).Decode(&aliases)
	if err != nil {
		return []string{}, err
	}

	return aliases, nil
}

func (u *Userli) GetDomain(ctx context.Context, domain string) (bool, error) {
	resp, err := u.call(ctx, fmt.Sprintf("%s/api/postfix/domain/%s", u.baseURL, domain))
	if err != nil {
		logger.Info("unable to process the domain", zap.String("domain", domain), zap.Error(err))
		return false, err
	}
	defer resp.Body.Close()

	var result bool
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return false, err
	}

	return result, nil
}

func (u *Userli) GetMailbox(ctx context.Context, email string) (bool, error) {
	sanitizedEmail, err := u.sanitizeEmail(email)
	if err != nil {
		logger.Info("unable to process the mailbox", zap.String("email", email), zap.Error(err))
		return false, nil
	}

	resp, err := u.call(ctx, fmt.Sprintf("%s/api/postfix/mailbox/%s", u.baseURL, sanitizedEmail))
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	var result bool
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return false, err
	}

	return result, nil
}

func (u *Userli) GetSenders(ctx context.Context, email string) ([]string, error) {
	sanitizedEmail, err := u.sanitizeEmail(email)
	if err != nil {
		logger.Info("unable to process the senders", zap.String("email", email), zap.Error(err))
		return []string{}, nil
	}

	resp, err := u.call(ctx, fmt.Sprintf("%s/api/postfix/senders/%s", u.baseURL, sanitizedEmail))
	if err != nil {
		return []string{}, err
	}
	defer resp.Body.Close()

	var senders []string
	err = json.NewDecoder(resp.Body).Decode(&senders)
	if err != nil {
		return []string{}, err
	}

	return senders, nil
}

func (u *Userli) GetQuota(ctx context.Context, email string) (*Quota, error) {
	sanitizedEmail, err := u.sanitizeEmail(email)
	if err != nil {
		log.WithError(err).WithField("email", email).Info("unable to process the quota")
		return nil, err
	}

	resp, err := u.call(ctx, fmt.Sprintf("%s/api/postfix/quota/%s", u.baseURL, sanitizedEmail))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var quota Quota
	err = json.NewDecoder(resp.Body).Decode(&quota)
	if err != nil {
		return nil, err
	}

	return &quota, nil
}

func (u *Userli) call(ctx context.Context, url string) (*http.Response, error) {
	startTime := time.Now()

	// Create request with context that has a timeout
	// If the parent context already has a deadline, use it
	// Otherwise, set a default timeout of 5 seconds for API calls
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	// Extract endpoint name from URL path for metrics
	endpoint := "unknown"
	if resp != nil {
		// Extract last part of path (alias, domain, mailbox, senders)
		parts := strings.Split(url, "/")
		if len(parts) >= 5 {
			endpoint = parts[len(parts)-2]
		}
	}

	statusCode := "error"
	if resp != nil {
		statusCode = fmt.Sprintf("%d", resp.StatusCode)
	}

	// Record HTTP client metrics
	duration := time.Since(startTime).Seconds()
	httpClientDuration.WithLabelValues(endpoint, statusCode).Observe(duration)
	httpClientRequestsTotal.WithLabelValues(endpoint, statusCode).Inc()

	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (u *Userli) sanitizeEmail(email string) (string, error) {
	// Normalize email: lowercase and remove whitespace
	email = strings.ToLower(email)
	email = strings.TrimSpace(email)

	// Remove all non-visible characters (control characters, zero-width spaces, etc.)
	email = strings.TrimFunc(email, func(r rune) bool {
		return r < 33 || r == 127 || // ASCII control characters
			r == 0x200B || // Zero-width space
			r == 0x200C || // Zero-width non-joiner
			r == 0x200D || // Zero-width joiner
			r == 0xFEFF // Zero-width no-break space (BOM)
	})

	// Split email by @
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid email format: %s", email)
	}

	localPart := parts[0]
	domain := parts[1]

	// Remove recipient delimiter from local part if configured
	if u.delimiter != "" {
		if idx := strings.Index(localPart, u.delimiter); idx != -1 {
			localPart = localPart[:idx]
		}
	}

	// Validate local part matches allowed pattern
	if !validLocalPartRegex.MatchString(localPart) {
		return "", fmt.Errorf("invalid local part: %s", localPart)
	}

	// Validate that local part is not empty
	if localPart == "" {
		return "", fmt.Errorf("invalid email format: empty local part after sanitization")
	}

	return fmt.Sprintf("%s@%s", localPart, domain), nil
}

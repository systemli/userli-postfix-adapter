package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type UserliService interface {
	GetAliases(email string) ([]string, error)
	GetDomain(domain string) (bool, error)
}

type Userli struct {
	token   string
	baseURL string

	Client *http.Client
}

func NewUserli(token, baseURL string) *Userli {
	client := &http.Client{
		Timeout: time.Second * 10,
	}

	return &Userli{token: token, baseURL: baseURL, Client: client}
}

func (u *Userli) GetAliases(email string) ([]string, error) {
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

func (u *Userli) call(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authentication", fmt.Sprintf("Bearer %s", u.token))

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "userli-postfix-adapter")

	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

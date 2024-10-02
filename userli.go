package main

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	return &Userli{token: token, baseURL: baseURL, Client: &http.Client{}}
}

func (u *Userli) GetAliases(email string) ([]string, error) {
	url := fmt.Sprintf("%s/api/postfix/alias/%s", u.baseURL, email)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return []string{}, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", u.token))
	resp, err := u.Client.Do(req)
	if err != nil {
		return []string{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []string{}, fmt.Errorf("error fetching aliases: %s", resp.Status)
	}

	var aliases []string
	err = json.NewDecoder(resp.Body).Decode(&aliases)
	if err != nil {
		return []string{}, err
	}

	return aliases, nil
}

func (u *Userli) GetDomain(domain string) (bool, error) {
	url := fmt.Sprintf("%s/api/postfix/domain/%s", u.baseURL, domain)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", u.token))
	resp, err := u.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("error fetching domain: %s", resp.Status)
	}

	var result bool
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return false, err
	}

	return result, nil
}

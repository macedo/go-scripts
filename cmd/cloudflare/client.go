package main

import (
	"fmt"
	"net/http"
)

type cloudflare struct {
	client http.Client
	token  string
}

func (c *cloudflare) Do(request *http.Request) (*http.Response, error) {
	request.Header = map[string][]string{
		"Authorization": {fmt.Sprintf("Bearer %s", c.token)},
	}
	return c.client.Do(request)
}

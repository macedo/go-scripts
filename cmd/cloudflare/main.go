package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/gojek/heimdall/v7/httpclient"
)

var (
	client        *httpclient.Client
	token, zoneID string
)

type ResponseData struct {
	Result []struct {
		ID        string `json:"id"`
		ZoneID    string `json:"zone_id"`
		ZoneName  string `json:"zone_name"`
		Name      string `json:"name"`
		Type      string `json:"type"`
		Content   string `json:"content"`
		Proxiable bool   `json:"proxiable"`
		Proxied   bool   `json:"proxied"`
		TTL       int    `json:"ttl"`
		Locked    bool   `json:"locked"`
		Meta      struct {
			AutoAdded           bool   `json:"auto_added"`
			ManagedByApps       bool   `json:"managed_by_apps"`
			ManagedByArgoTunnel bool   `json:"managed_by_argo_tunnel"`
			Source              string `json:"source"`
		} `json:"meta"`
		CreatedOn  time.Time `json:"created_on"`
		ModifiedOn time.Time `json:"modified_on"`
	} `json:"result"`
	Success    bool          `json:"success"`
	Errors     []interface{} `json:"errors"`
	Messages   []interface{} `json:"messages"`
	ResultInfo struct {
		Page       int `json:"page"`
		PerPage    int `json:"per_page"`
		Count      int `json:"count"`
		TotalCount int `json:"total_count"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func init() {
	flag.StringVar(&token, "token", "", "authentication bearer token")
	flag.StringVar(&zoneID, "zoneid", "", "zone id")
	flag.Parse()

	client = httpclient.NewClient(
		httpclient.WithHTTPClient(&cloudflare{
			client: http.Client{Timeout: 5 * time.Second},
			token:  token,
		}),
	)
}

func main() {
	doneCh := make(chan struct{})
	errCh := make(chan error)
	idsCh := make(chan string)

	wg := sync.WaitGroup{}

	go func() {
		defer close(idsCh)

		response, err := client.Get(fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?per_page=1000", zoneID), nil)
		if err != nil {
			errCh <- err
			return
		}
		defer response.Body.Close()

		b, err := ioutil.ReadAll(response.Body)
		if err != nil {
			errCh <- err
			return
		}

		var responseData ResponseData
		json.Unmarshal(b, &responseData)

		for _, record := range responseData.Result {
			idsCh <- record.ID
		}
	}()

	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range idsCh {
				_, err := client.Delete(fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, id), nil)
				if err != nil {
					errCh <- err
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(doneCh)
	}()

	for {
		select {
		case err := <-errCh:
			log.Fatal(err)
		case <-doneCh:
			fmt.Println("finished")
		}
	}
}

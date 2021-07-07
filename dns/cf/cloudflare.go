package cf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cloudflare/cloudflare-go"
	"github.com/hashicorp/go-multierror"
)

type Config struct {
	IP       string `long:"ip" env:"IP" description:"Public IP address for DNS record. If not defined - will be detected automatically by myexternalip.com"`
	Proxy    bool   `long:"proxy" env:"PROXY" description:"Let Cloudflare proxy traffic. Implies some level of protection and automatic SSL between client and Cloudflare"`
	APIToken string `long:"api-token" env:"API_TOKEN" description:"API token"`
}

func New(ctx context.Context, config Config) (*CloudFlare, error) {
	client, err := cloudflare.NewWithAPIToken(config.APIToken)
	if err != nil {
		return nil, fmt.Errorf("create cloudflare token: %w", err)
	}

	if config.IP == "" {
		ip, err := getMyIP(ctx)
		if err != nil {
			return nil, fmt.Errorf("get self IP: %w", err)
		}
		config.IP = ip
	}

	return &CloudFlare{api: client, config: config}, nil
}

type CloudFlare struct {
	api    *cloudflare.API
	config Config
}

func (cf *CloudFlare) Register(ctx context.Context, domains []string) error {
	var zoneIDS = map[string]string{}
	for _, domain := range domains {
		_, zone := splitNameZone(domain)
		_, ok := zoneIDS[zone]
		if ok {
			continue
		}
		zoneID, err := cf.api.ZoneIDByName(zone)
		if err != nil {
			return fmt.Errorf("get zone ID by name %s: %w", zone, err)
		}
		zoneIDS[zone] = zoneID
	}

	var records = map[string]string{}
	for _, zoneID := range zoneIDS {
		list, err := cf.api.DNSRecords(ctx, zoneID, cloudflare.DNSRecord{Type: "A"})
		if err != nil {
			return fmt.Errorf("list zone records: %w", err)
		}

		for _, item := range list {
			records[item.Name] = item.ID
		}
	}
	for _, domain := range domains {
		name, zone := splitNameZone(domain)
		zoneID := zoneIDS[zone]
		record := cloudflare.DNSRecord{
			Type:    "A",
			Name:    name,
			Content: cf.config.IP,
			TTL:     1,
			Proxied: &cf.config.Proxy,
		}

		recordID := records[domain]

		if recordID != "" {
			if err := cf.api.UpdateDNSRecord(ctx, zoneID, recordID, record); err != nil {
				return fmt.Errorf("update record %s: %w", domain, err)
			}
		} else {
			res, err := cf.api.CreateDNSRecord(ctx, zoneID, record)
			if err != nil {
				return fmt.Errorf("request to create A record %s (zone: %s, name: %s): %w", domain, zone, name, err)
			}

			if !res.Success {
				return fmt.Errorf("create A record %s: %w", domain, aggregateErrors(res.Errors))
			}
		}
	}

	return nil
}

func splitNameZone(domain string) (string, string) {
	const zoneSize = 2

	var num = 0
	for i := len(domain) - 1; i >= 0; i-- {
		if domain[i] == '.' {
			num++
		}
		if num == zoneSize {
			return domain[:i], domain[i+1:]
		}
	}

	return "", domain
}

func aggregateErrors(res []cloudflare.ResponseInfo) error {
	var mr error

	for _, e := range res {
		mr = multierror.Append(mr, &ErrCloudFlare{Code: e.Code, Message: e.Message})
	}

	return fmt.Errorf("cloudflare error: %w", mr)
}

func getMyIP(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://myexternalip.com/json", nil)
	if err != nil {
		return "", fmt.Errorf("preapre request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return "", &ErrHTTP{Code: res.StatusCode, Status: res.Status}
	}

	var info struct {
		IP string `json:"ip"`
	}

	err = json.NewDecoder(res.Body).Decode(&info)
	if err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return info.IP, nil
}

type ErrCloudFlare struct {
	Code    int
	Message string
}

func (ecf *ErrCloudFlare) Error() string {
	return fmt.Sprint(ecf.Code, ": ", ecf.Message)
}

type ErrHTTP struct {
	Code   int
	Status string
}

func (eh *ErrHTTP) Error() string {
	return fmt.Sprint(eh.Code, " ", eh.Status)
}

// This read-only helper is built against the pre-feature source to qualify
// restored ownership with that revision's provider adapters and parser.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/maxfield-allison/dnsweaver/pkg/provider"
	"github.com/maxfield-allison/dnsweaver/providers/cloudflare"
	"github.com/maxfield-allison/dnsweaver/providers/technitium"
)

func run() error {
	var cfg struct {
		Provider, Zone, ZoneID, URL, TokenFile, Prefix string
		RecordType                                     provider.RecordType
		Owners                                         []string
	}
	data, err := os.ReadFile(os.Getenv("DNSWEAVER_LIVE_CONFIG"))
	if err != nil {
		return err
	}
	if decodeErr := json.Unmarshal(data, &cfg); decodeErr != nil {
		return decodeErr
	}
	token, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var backend provider.Provider
	switch cfg.Provider {
	case "cloudflare":
		backend, err = cloudflare.New("old-reader", &cloudflare.Config{Zone: cfg.Zone, ZoneID: cfg.ZoneID, Token: strings.TrimSpace(string(token)), TTL: 300}, cloudflare.WithProviderLogger(logger))
	case "technitium":
		backend, err = technitium.New("old-reader", &technitium.Config{URL: cfg.URL, Zone: cfg.Zone, Token: strings.TrimSpace(string(token)), TTL: 300}, technitium.WithProviderLogger(logger))
	default:
		return fmt.Errorf("unsupported provider")
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	records, err := backend.List(ctx)
	if err != nil {
		return err
	}
	result := struct{ Claims, Answers int }{}
	for _, record := range records {
		if !strings.HasSuffix(record.Hostname, "."+cfg.Prefix+"."+cfg.Zone) {
			continue
		}
		if record.Type == cfg.RecordType {
			result.Answers++
		}
		if provider.IsOwnershipRecord(record.Hostname) {
			for _, owner := range cfg.Owners {
				if provider.MatchesOwnership(record.Target, owner) {
					result.Claims++
					break
				}
			}
		}
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := run(); err != nil {
		// Never print an untrusted credential-bearing transport error.
		fmt.Fprintln(os.Stderr, "old-source provider read failed")
		os.Exit(1)
	}
}

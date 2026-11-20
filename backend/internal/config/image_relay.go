package config

import (
	"encoding/hex"
	"errors"
	"net/url"
	"path/filepath"
	"regexp"

	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
)

var imageRelaySiteLabel = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,31}$")

func (c ImageRelayConfig) validate(responseLimit int64) error {
	if c.Role == "" {
		return nil
	}
	if c.Role != "main" && c.Role != "l1" {
		return errors.New("gateway.image_relay.role must be main or l1")
	}
	if c.MainAPIKeyID <= 0 || !filepath.IsAbs(c.PrivateDir) || !imageRelaySiteLabel.MatchString(c.SourceSite) ||
		!imageRelaySiteLabel.MatchString(c.DestinationSite) || c.SourceSite == c.DestinationSite {
		return errors.New("gateway.image_relay requires different site identities, a main API key ID and an absolute private directory")
	}
	secret, err := hex.DecodeString(c.SecretHex)
	if err != nil || len(secret) != 32 {
		return errors.New("gateway.image_relay.secret_hex must encode a 32-byte secret")
	}
	if _, err = imagerelay.NewSigner(c.KeyID, map[string][]byte{c.KeyID: secret}); err != nil {
		return errors.New("gateway.image_relay.key_id is invalid")
	}
	if c.Role == "l1" {
		origin, err := url.Parse(c.MainOrigin)
		if err != nil || origin.Hostname() == "" || origin.User != nil ||
			(origin.Scheme != "https" && origin.Scheme != "http") ||
			origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" ||
			c.L1AccountID <= 0 || responseLimit < 1 || responseLimit > DefaultUpstreamResponseReadMaxBytes {
			return errors.New("gateway.image_relay l1 requires an exact upstream origin, a dedicated account ID and a bounded response size")
		}
	}
	return nil
}

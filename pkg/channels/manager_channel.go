package channels

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/dapicom-ai/omnipus/pkg/config"
	"github.com/dapicom-ai/omnipus/pkg/logger"
)

// Note: hiddenValues and updateKeys were removed. Previously they re-injected *Ref fields
// into hash/config maps because the old SecureString type returned "[NOT_HERE]" from
// MarshalJSON, silently erasing secret refs on JSON round-trips. Now that all secret
// fields are plain strings (*Ref), they survive JSON marshal/unmarshal without any
// special handling.

func toChannelHashes(cfg *config.Config) map[string]string {
	result := make(map[string]string)
	ch := cfg.Channels
	marshal, err := json.Marshal(ch)
	if err != nil {
		logger.ErrorCF(
			"channels",
			"toChannelHashes: failed to marshal channel config",
			map[string]any{"error": err.Error()},
		)
		return result
	}
	var channelConfig map[string]map[string]any
	if err := json.Unmarshal(marshal, &channelConfig); err != nil {
		logger.ErrorCF(
			"channels",
			"toChannelHashes: failed to unmarshal channel config",
			map[string]any{"error": err.Error()},
		)
		return result
	}

	for key, value := range channelConfig {
		enabled, _ := value["enabled"].(bool)
		if !enabled {
			continue
		}
		valueBytes, _ := json.Marshal(value)
		hash := md5.Sum(valueBytes)
		result[key] = hex.EncodeToString(hash[:])
	}

	return result
}

func compareChannels(old, news map[string]string) (added, removed []string) {
	for key, newHash := range news {
		if oldHash, ok := old[key]; ok {
			if newHash != oldHash {
				removed = append(removed, key)
				added = append(added, key)
			}
		} else {
			added = append(added, key)
		}
	}
	for key := range old {
		if _, ok := news[key]; !ok {
			removed = append(removed, key)
		}
	}
	return added, removed
}

func toChannelConfig(cfg *config.Config, list []string) (*config.ChannelsConfig, error) {
	result := &config.ChannelsConfig{}
	ch := cfg.Channels
	marshal, err := json.Marshal(ch)
	if err != nil {
		logger.ErrorCF(
			"channels",
			"toChannelConfig: failed to marshal channel config",
			map[string]any{"error": err.Error()},
		)
		return nil, fmt.Errorf("toChannelConfig: marshal: %w", err)
	}
	var channelConfig map[string]map[string]any
	if unmarshalErr := json.Unmarshal(marshal, &channelConfig); unmarshalErr != nil {
		logger.ErrorCF(
			"channels",
			"toChannelConfig: failed to unmarshal channel config",
			map[string]any{"error": unmarshalErr.Error()},
		)
		return nil, fmt.Errorf("toChannelConfig: unmarshal: %w", unmarshalErr)
	}
	temp := make(map[string]map[string]any, 0)

	for key, value := range channelConfig {
		found := false
		for _, s := range list {
			if key == s {
				found = true
				break
			}
		}
		chEnabled, _ := value["enabled"].(bool)
		if !found || !chEnabled {
			continue
		}
		temp[key] = value
	}

	marshal, err = json.Marshal(temp)
	if err != nil {
		logger.Errorf("marshal error: %v", err)
		return nil, err
	}
	err = json.Unmarshal(marshal, result)
	if err != nil {
		logger.Errorf("unmarshal error: %v", err)
		return nil, err
	}

	return result, nil
}


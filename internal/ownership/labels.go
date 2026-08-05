package ownership

import (
	"encoding/base64"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"
)

const (
	ManagedByKey            = "managed-by"
	ManagedByValue          = "serverpro"
	NamespaceKey            = "serverpro.namespace"
	ServerKey               = "serverpro.server"
	ProviderTagNamespaceKey = "serverpro-namespace"
	ProviderTagServerKey    = "serverpro-server"
)

const providerTagEncodedPrefix = "b64:"

var (
	providerTagKeyInvalidChars   = regexp.MustCompile(`[^A-Za-z0-9_-]`)
	providerTagValueInvalidChars = regexp.MustCompile(`[^A-Za-z0-9:_-]`)
)

func ConfigLabels(namespace, server string, extra map[string]string) map[string]string {
	labels := copyLabels(extra)
	delete(labels, "project")
	delete(labels, "server")
	labels[ManagedByKey] = ManagedByValue
	if namespace != "" {
		labels[NamespaceKey] = namespace
	}
	if server != "" {
		labels[ServerKey] = server
	}
	return labels
}

func ProviderLabels(namespace, server string, extra map[string]string) map[string]string {
	labels := map[string]string{}
	for key, value := range extra {
		if reservedProviderLabelKey(key) {
			continue
		}
		labels[providerTagKey(key)] = value
	}
	labels[ManagedByKey] = ManagedByValue
	labels[ProviderTagNamespaceKey] = namespace
	labels[ProviderTagServerKey] = server
	return labels
}

func LiveLabelsMatch(labels map[string]string, namespace, server string) bool {
	return ValidateLiveLabels(labels, namespace, server) == nil
}

// OwnershipFromLabels recovers namespace/server from live provider ownership labels.
// WHY: import/discover rebuild local SoT only from resources serverpro previously stamped.
func OwnershipFromLabels(labels map[string]string) (namespace, server string, managed bool) {
	if len(labels) == 0 {
		return "", "", false
	}
	canonical := canonicalProviderLabels(labels)
	if canonical[ManagedByKey] != ManagedByValue {
		return "", "", false
	}
	namespace = canonical[NamespaceKey]
	server = canonical[ServerKey]
	if namespace == "" || server == "" {
		return "", "", false
	}
	return namespace, server, true
}

func ProviderTags(labels map[string]string) []string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tags := make([]string, 0, len(keys))
	for _, key := range keys {
		tags = append(tags, fmt.Sprintf("%s:%s", providerTagKey(key), providerTagValue(labels[key])))
	}
	return tags
}

func LabelsFromProviderTags(tags []string) map[string]string {
	labels := make(map[string]string, len(tags))
	for _, tag := range tags {
		key, value, ok := strings.Cut(tag, ":")
		if !ok {
			continue
		}
		labels[labelKeyFromProviderTag(key)] = decodeProviderTagValue(value)
	}
	return labels
}

func ValidateLiveLabels(labels map[string]string, namespace, server string) error {
	if len(labels) == 0 {
		return fmt.Errorf("provider ownership mismatch: live resource has no ownership labels")
	}
	canonical := canonicalProviderLabels(labels)
	if got := canonical[ManagedByKey]; got != ManagedByValue {
		return fmt.Errorf("provider ownership mismatch: live managed-by label %q", got)
	}
	if err := validateLabel(canonical, NamespaceKey, namespace); err != nil {
		return err
	}
	return validateLabel(canonical, ServerKey, server)
}

func reservedProviderLabelKey(key string) bool {
	switch key {
	case "project", "server", ManagedByKey, NamespaceKey, ServerKey, ProviderTagNamespaceKey, ProviderTagServerKey:
		return true
	default:
		return false
	}
}

func canonicalProviderLabels(labels map[string]string) map[string]string {
	canonical := copyLabels(labels)
	if value, ok := labels[ProviderTagNamespaceKey]; ok {
		canonical[NamespaceKey] = decodeProviderTagValue(value)
	}
	if value, ok := labels[ProviderTagServerKey]; ok {
		canonical[ServerKey] = decodeProviderTagValue(value)
	}
	return canonical
}

func providerTagKey(key string) string {
	switch key {
	case NamespaceKey, ProviderTagNamespaceKey:
		return ProviderTagNamespaceKey
	case ServerKey, ProviderTagServerKey:
		return ProviderTagServerKey
	default:
		return providerTagPart(key)
	}
}

func labelKeyFromProviderTag(key string) string {
	switch key {
	case ProviderTagNamespaceKey:
		return NamespaceKey
	case ProviderTagServerKey:
		return ServerKey
	default:
		return key
	}
}

func providerTagValue(value string) string {
	if value == "" || (!providerTagValueInvalidChars.MatchString(value) && !strings.HasPrefix(value, providerTagEncodedPrefix)) {
		return value
	}
	return providerTagEncodedPrefix + base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodeProviderTagValue(value string) string {
	if !strings.HasPrefix(value, providerTagEncodedPrefix) {
		return value
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, providerTagEncodedPrefix))
	if err != nil {
		return value
	}
	return string(decoded)
}

func providerTagPart(value string) string {
	return providerTagKeyInvalidChars.ReplaceAllString(value, "-")
}

func copyLabels(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+3)
	maps.Copy(out, in)
	return out
}

func validateLabel(labels map[string]string, key, want string) error {
	if want == "" {
		return fmt.Errorf("provider ownership mismatch: state %s missing", key)
	}
	got, ok := labels[key]
	if !ok {
		return fmt.Errorf("provider ownership mismatch: live %s label missing", key)
	}
	if got != want {
		return fmt.Errorf("provider ownership mismatch: live %s label %q state %q", key, got, want)
	}
	return nil
}

package provider

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const (
	ownershipManifestPrefix = "dnsweaver-state=v1:"
	ownershipPagePrefix     = "dnsweaver-page=v1:"
	ownershipManifestLabel  = "_dnsweaver-state"
	ownershipPageLabel      = "_dw-p"
	// DefaultOwnershipPageBytes bounds encoded TXT content, not entry count.
	DefaultOwnershipPageBytes = 3500
	maxOwnershipExpandedBytes = 256 * 1024
	maxOwnershipPages         = 64
	maxOwnershipEntries       = 16384
)

// ErrOwnershipState indicates incomplete, corrupt or competing ownership state.
// Callers must preserve DNS data when a snapshot cannot be validated.
var ErrOwnershipState = errors.New("ownership state is unavailable or ambiguous")

// OwnershipEntry is the serialized ownership contract. Name is a normalized
// legacy marker name; Value retains exact membership and metadata without an API ID.
type OwnershipEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// OwnershipManifest publishes one complete immutable page set for a logical owner.
// Previous is the SHA-256 digest of the preceding encoded manifest, not a timestamp.
type OwnershipManifest struct {
	Version  int      `json:"version"`
	Owner    string   `json:"owner"`
	Revision uint64   `json:"revision"`
	Previous string   `json:"previous"`
	Pages    []string `json:"pages"`
	Entries  int      `json:"entries"`
}

// OwnershipManifestName is the common discovery anchor. Each owner adds/removes
// its own exact TXT value; no writer replaces this entire RRset.
func OwnershipManifestName(zone string) string {
	return ownershipManifestLabel + "." + normalizeOwnershipName(zone)
}

// OwnershipOwnerManifestName isolates each owner's manifest RRset on APIs that
// enumerate zones, including those whose writes replace the entire RRset.
func OwnershipOwnerManifestName(zone, owner string) string {
	return ownershipManifestLabel + "." + ownershipDigest(owner)[:16] + "." + normalizeOwnershipName(zone)
}

// IsOwnershipStateRecord recognizes reserved compact-state names. They must not
// be interpreted as ordinary source-driven TXT records or orphan data.
func IsOwnershipStateRecord(name string) bool {
	name = normalizeOwnershipName(name)
	return strings.HasPrefix(name, ownershipManifestLabel+".") || strings.HasPrefix(name, ownershipPageLabel+".")
}

// OwnershipPageName derives a provider-independent page name from its owner and
// full content digest. The content is still checked against the full SHA-256.
func OwnershipPageName(zone, owner, digest string) string {
	if !validOwnershipDigest(digest) {
		return ""
	}
	ownerDigest := ownershipDigest(owner)
	return ownershipPageLabel + "." + ownerDigest[:16] + "." + digest[:32] + "." + normalizeOwnershipName(zone)
}

func normalizeOwnershipName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, "."))
}

func ownershipDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validOwnershipDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func validateOwnershipEntry(entry OwnershipEntry, owner string) error {
	if entry.Name != normalizeOwnershipName(entry.Name) || !validOwnershipName(entry.Name) || !IsOwnershipRecord(entry.Name) || IsOwnershipStateRecord(entry.Name) {
		return fmt.Errorf("%w: invalid ownership entry name", ErrOwnershipState)
	}
	owned, id, fields := ParseOwnershipValue(entry.Value)
	if !owned || id != owner || ExtractHostnameFromOwnership(entry.Name) == "" {
		return fmt.Errorf("%w: ownership entry has a different owner", ErrOwnershipState)
	}
	if _, versioned := fields[keyRecordVersion]; versioned {
		_, _, member, _ := ParseMemberOwnershipValue(entry.Value)
		if member == nil {
			return fmt.Errorf("%w: malformed exact-member ownership", ErrOwnershipState)
		}
	}
	return nil
}

func validOwnershipName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		for _, b := range []byte(label) {
			if b != '-' && b != '_' && b != '*' && (b < 'a' || b > 'z') && (b < '0' || b > '9') {
				return false
			}
		}
	}
	return true
}

func canonicalOwnershipEntries(entries []OwnershipEntry, owner string) ([]OwnershipEntry, error) {
	if len(entries) > maxOwnershipEntries {
		return nil, fmt.Errorf("%w: too many ownership entries", ErrOwnershipState)
	}
	result := append([]OwnershipEntry{}, entries...)
	for _, entry := range result {
		if err := validateOwnershipEntry(entry, owner); err != nil {
			return nil, err
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Value < result[j].Value
	})
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil, fmt.Errorf("%w: duplicate ownership entry", ErrOwnershipState)
		}
	}
	return result, nil
}

func encodeOwnershipPage(entries []OwnershipEntry) (string, error) {
	payload, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("encoding ownership page: %w", err)
	}
	if len(payload) > maxOwnershipExpandedBytes {
		return "", fmt.Errorf("%w: expanded ownership page is too large", ErrOwnershipState)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		return "", fmt.Errorf("compressing ownership page: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("finishing ownership page: %w", err)
	}
	return ownershipPagePrefix + base64.RawURLEncoding.EncodeToString(compressed.Bytes()), nil
}

func decodeOwnershipPage(value, owner string, maxBytes int) ([]OwnershipEntry, error) {
	if len(value) > maxBytes || !strings.HasPrefix(value, ownershipPagePrefix) {
		return nil, fmt.Errorf("%w: invalid ownership page envelope", ErrOwnershipState)
	}
	compressed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, ownershipPagePrefix))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ownership page encoding", ErrOwnershipState)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid ownership page compression", ErrOwnershipState)
	}
	payload, readErr := io.ReadAll(io.LimitReader(reader, maxOwnershipExpandedBytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || len(payload) > maxOwnershipExpandedBytes {
		return nil, fmt.Errorf("%w: unreadable or oversized ownership page", ErrOwnershipState)
	}
	var entries []OwnershipEntry
	if err := decodeOwnershipJSON(payload, &entries); err != nil || entries == nil || len(entries) == 0 {
		return nil, fmt.Errorf("%w: invalid ownership page entries", ErrOwnershipState)
	}
	return canonicalOwnershipEntries(entries, owner)
}

func encodeOwnershipManifest(manifest OwnershipManifest) (string, error) {
	if err := validateOwnershipManifest(manifest); err != nil {
		return "", err
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encoding ownership manifest: %w", err)
	}
	value := ownershipManifestPrefix + base64.RawURLEncoding.EncodeToString(payload)
	if len(value) > DefaultOwnershipPageBytes {
		return "", fmt.Errorf("%w: ownership manifest is too large", ErrOwnershipState)
	}
	return value, nil
}

// DecodeOwnershipManifest validates an encoded discovery record. It is shared
// with providers that must query referenced pages without zone enumeration.
func DecodeOwnershipManifest(value string) (OwnershipManifest, error) {
	var manifest OwnershipManifest
	if len(value) > DefaultOwnershipPageBytes || !strings.HasPrefix(value, ownershipManifestPrefix) {
		return manifest, fmt.Errorf("%w: unsupported ownership manifest", ErrOwnershipState)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, ownershipManifestPrefix))
	if err != nil {
		return manifest, fmt.Errorf("%w: invalid manifest encoding", ErrOwnershipState)
	}
	var fields map[string]json.RawMessage
	if fieldErr := json.Unmarshal(payload, &fields); fieldErr != nil {
		return manifest, fmt.Errorf("%w: invalid manifest object", ErrOwnershipState)
	}
	for _, name := range []string{"version", "owner", "revision", "previous", "pages", "entries"} {
		field, found := fields[name]
		if !found || bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
			return manifest, fmt.Errorf("%w: missing manifest field %s", ErrOwnershipState, name)
		}
	}
	if err := decodeOwnershipJSON(payload, &manifest); err != nil {
		return manifest, fmt.Errorf("%w: invalid manifest JSON", ErrOwnershipState)
	}
	return manifest, validateOwnershipManifest(manifest)
}

func decodeOwnershipJSON(payload []byte, target any) error {
	if err := rejectDuplicateOwnershipFields(json.NewDecoder(bytes.NewReader(payload)), 0); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}

func rejectDuplicateOwnershipFields(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return fmt.Errorf("ownership JSON nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			key, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate ownership JSON field")
			}
			seen[name] = true
			if fieldErr := rejectDuplicateOwnershipFields(decoder, depth+1); fieldErr != nil {
				return fieldErr
			}
		}
	case '[':
		for decoder.More() {
			if entryErr := rejectDuplicateOwnershipFields(decoder, depth+1); entryErr != nil {
				return entryErr
			}
		}
	default:
		return fmt.Errorf("unexpected ownership JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func validateOwnershipManifest(manifest OwnershipManifest) error {
	if manifest.Version != 1 || manifest.Revision == 0 || len(manifest.Owner) > 63 || strings.ContainsAny(manifest.Owner, ",=\r\n") || manifest.Pages == nil || len(manifest.Pages) > maxOwnershipPages || manifest.Entries < 0 || manifest.Entries > maxOwnershipEntries {
		return fmt.Errorf("%w: invalid ownership manifest fields", ErrOwnershipState)
	}
	if (manifest.Revision == 1) != (manifest.Previous == "") || (manifest.Previous != "" && !validOwnershipDigest(manifest.Previous)) || (manifest.Entries == 0) != (len(manifest.Pages) == 0) {
		return fmt.Errorf("%w: invalid ownership manifest history", ErrOwnershipState)
	}
	seen := make(map[string]bool, len(manifest.Pages))
	for _, digest := range manifest.Pages {
		if !validOwnershipDigest(digest) || seen[digest] {
			return fmt.Errorf("%w: invalid or duplicate page digest", ErrOwnershipState)
		}
		seen[digest] = true
	}
	return nil
}

func packOwnershipPages(zone, owner string, entries []OwnershipEntry, maxBytes int) ([]Record, error) {
	if maxBytes < 256 || maxBytes > DefaultOwnershipPageBytes {
		return nil, fmt.Errorf("ownership page byte limit must be between 256 and %d", DefaultOwnershipPageBytes)
	}
	entries, err := canonicalOwnershipEntries(entries, owner)
	if err != nil {
		return nil, err
	}
	zone = normalizeOwnershipName(zone)
	if !validOwnershipName(zone) {
		return nil, fmt.Errorf("%w: invalid ownership zone", ErrOwnershipState)
	}
	for _, entry := range entries {
		host := ExtractHostnameFromOwnership(entry.Name)
		if host != zone && !strings.HasSuffix(host, "."+zone) {
			return nil, fmt.Errorf("%w: ownership entry is outside the zone", ErrOwnershipState)
		}
	}
	var pages []Record
	for start := 0; start < len(entries); {
		end := start
		value := ""
		for end < len(entries) {
			candidate, encodeErr := encodeOwnershipPage(entries[start : end+1])
			if encodeErr != nil || len(candidate) > maxBytes {
				break
			}
			value = candidate
			end++
		}
		if end == start {
			return nil, fmt.Errorf("%w: one ownership entry exceeds page limits", ErrOwnershipState)
		}
		name := OwnershipPageName(zone, owner, ownershipDigest(value))
		if len(name) > 253 {
			return nil, fmt.Errorf("%w: ownership page DNS name exceeds 253 bytes", ErrOwnershipState)
		}
		pages = append(pages, Record{Hostname: name, Type: RecordTypeTXT, Target: value, TTL: 300})
		if len(pages) > maxOwnershipPages {
			return nil, fmt.Errorf("%w: too many ownership pages", ErrOwnershipState)
		}
		start = end
	}
	return pages, nil
}

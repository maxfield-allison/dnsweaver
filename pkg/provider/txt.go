package provider

import (
	"strconv"
	"strings"
)

// DecodeTXTStrings concatenates a TXT value written as a sequence of quoted
// character strings (RFC 1035 section 5.1), such as `"v=spf1 -all"` or
// `"part one" "part two"`. It reports false when value does not start with a
// quote or is not a well-formed sequence, in which case callers keep the
// value as given.
func DecodeTXTStrings(value string) (string, bool) {
	if !strings.HasPrefix(value, "\"") {
		return "", false
	}
	var content strings.Builder
	rest := value
	for rest != "" {
		if rest[0] != '"' {
			return "", false
		}
		end := 1
		for end < len(rest) {
			if rest[end] == '\\' {
				end += 2
				continue
			}
			if rest[end] == '"' {
				break
			}
			end++
		}
		if end >= len(rest) {
			return "", false
		}
		decoded, err := strconv.Unquote(rest[:end+1])
		if err != nil {
			return "", false
		}
		content.WriteString(decoded)
		rest = strings.TrimSpace(rest[end+1:])
	}
	return content.String(), true
}

// TXTPresentationText is implemented by providers whose API stores TXT
// content as presentation text, so a declared value such as `"v=spf1 -all"`
// is quoted presentation of the data v=spf1 -all. For those providers TXT
// Record.Target holds the decoded data, and the provider converts it at its
// API boundary. Other providers store declared TXT values verbatim.
type TXTPresentationText interface {
	TXTPresentationText()
}

// DeclaredTXT returns the TXT Record.Target a declared value stands for on p.
func DeclaredTXT(p Provider, value string) string {
	if _, ok := p.(TXTPresentationText); ok {
		return TXTPayload(value)
	}
	return value
}

// TXTPayload returns the character data a TXT value in presentation text
// stands for. A value written as quoted strings, such as `"v=spf1 -all"` or
// `"\"hello\""`, is decoded once; any other value already is the data.
func TXTPayload(value string) string {
	if decoded, ok := DecodeTXTStrings(value); ok {
		return decoded
	}
	return value
}

// TXTPresentation is the inverse of TXTPayload for providers whose API takes
// presentation text: data that TXTPayload would decode again, such as
// `"hello"` with its quotes, is written as quoted strings of at most 255
// bytes, and any other data is returned unchanged.
func TXTPresentation(payload string) string {
	if _, ok := DecodeTXTStrings(payload); !ok {
		return payload
	}
	var parts []string
	for len(payload) > 255 {
		parts = append(parts, strconv.Quote(payload[:255]))
		payload = payload[255:]
	}
	return strings.Join(append(parts, strconv.Quote(payload)), " ")
}

// MapMemberOwnershipTXTTarget returns the member marker value with the target
// of a TXT member replaced by f(target). Any other value, and a TXT marker
// whose target f leaves unchanged, is returned as given. Providers that
// implement TXTPresentationText use it to keep marker targets in the same
// presentation form as the TXT content they store.
func MapMemberOwnershipTXTTarget(value string, f func(string) string) string {
	owned, instanceID, fields := ParseOwnershipValue(value)
	if !owned || fields[keyRecordVersion] != memberOwnershipVersion || fields[keyRecordType] != string(RecordTypeTXT) {
		return value
	}
	target, err := decodeOwnershipField(fields[keyRecordTarget])
	if err != nil {
		return value
	}
	mapped := f(target)
	if mapped == target {
		return value
	}
	fields[keyRecordTarget] = encodeOwnershipField(mapped)
	return MakeOwnershipValue(instanceID, fields)
}

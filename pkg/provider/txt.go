package provider

import "strings"

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
		decoded, consumed, ok := decodeTXTString(rest)
		if !ok {
			return "", false
		}
		content.WriteString(decoded)
		rest = strings.TrimSpace(rest[consumed:])
	}
	return content.String(), true
}

func decodeTXTString(value string) (string, int, bool) {
	if value == "" || value[0] != '"' {
		return "", 0, false
	}
	var content strings.Builder
	for i := 1; i < len(value); i++ {
		switch value[i] {
		case '"':
			return content.String(), i + 1, true
		case '\\':
			i++
			if i == len(value) {
				return "", 0, false
			}
			// DNS presentation uses decimal octets, not Go's octal or
			// hexadecimal escapes. Other escaped characters are literal.
			if i+2 < len(value) && isTXTDigit(value[i]) && isTXTDigit(value[i+1]) && isTXTDigit(value[i+2]) {
				octet := int(value[i]-'0')*100 + int(value[i+1]-'0')*10 + int(value[i+2]-'0')
				if octet > 255 {
					return "", 0, false
				}
				content.WriteByte(byte(octet))
				i += 2
			} else {
				content.WriteByte(value[i])
			}
		default:
			content.WriteByte(value[i])
		}
	}
	return "", 0, false
}

func isTXTDigit(value byte) bool { return value >= '0' && value <= '9' }

// TXTPresentationText is implemented by providers whose API stores TXT
// content as presentation text, so a declared value such as `"v=spf1 -all"`
// is quoted presentation of the data v=spf1 -all. For those providers TXT
// Record.Target holds the decoded data, and the provider converts it at its
// API boundary. Other providers store declared TXT values verbatim.
type TXTPresentationText interface {
	TXTPresentationText()
}

// DeclaredTXT returns the TXT Record.Target a declared value stands for on p.
// Decorators expose their backend with Unwrap.
func DeclaredTXT(p Provider, value string) string {
	for p != nil {
		if _, ok := p.(TXTPresentationText); ok {
			return TXTPayload(value)
		}
		wrapper, ok := p.(interface{ Unwrap() Provider })
		if !ok {
			break
		}
		p = wrapper.Unwrap()
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

// TXTPresentation protects empty data and data beginning with a quote from
// being interpreted as presentation syntax. It uses RFC 1035 escapes and
// character strings of at most 255 payload bytes; other data is unchanged.
func TXTPresentation(payload string) string {
	if payload != "" && !strings.HasPrefix(payload, "\"") {
		return payload
	}
	var parts []string
	for len(payload) > 255 {
		parts = append(parts, quoteTXTString(payload[:255]))
		payload = payload[255:]
	}
	return strings.Join(append(parts, quoteTXTString(payload)), " ")
}

func quoteTXTString(value string) string {
	var quoted strings.Builder
	quoted.WriteByte('"')
	for i := 0; i < len(value); i++ {
		b := value[i]
		switch {
		case b == '"' || b == '\\':
			quoted.WriteByte('\\')
			quoted.WriteByte(b)
		case b < 32 || b >= 127:
			// Decimal byte escapes also preserve UTF-8 split across chunks.
			quoted.WriteByte('\\')
			quoted.WriteByte('0' + b/100)
			quoted.WriteByte('0' + b/10%10)
			quoted.WriteByte('0' + b%10)
		default:
			quoted.WriteByte(b)
		}
	}
	quoted.WriteByte('"')
	return quoted.String()
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

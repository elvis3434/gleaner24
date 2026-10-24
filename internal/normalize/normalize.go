// Package normalize canonicalises URLs, text and file names.
package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// trackingParams are query parameters dropped from URLs; any parameter
// starting with "utm_" is dropped as well.
var trackingParams = map[string]bool{
	"fbclid": true, "gclid": true, "dclid": true, "gbraid": true, "wbraid": true,
	"msclkid": true, "yclid": true, "igshid": true, "mc_cid": true, "mc_eid": true,
	"_ga": true, "_gl": true, "_hsenc": true, "_hsmi": true, "mkt_tok": true,
}

// URL returns the canonical form of raw: lower-case scheme and host, no
// default port, no fragment, no tracking parameters and the remaining query
// parameters sorted. Only absolute http(s) URLs are accepted.
func URL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return "", errors.New("URL has no host")
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	switch {
	case port != "":
		u.Host = net.JoinHostPort(host, port) // brackets IPv6 hosts
	case strings.Contains(host, ":"):
		u.Host = "[" + host + "]"
	default:
		u.Host = host
	}
	u.User = nil
	u.Fragment = ""
	u.RawFragment = ""
	if u.Path == "" {
		u.Path = "/"
		u.RawPath = ""
	}
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if trackingParams[strings.ToLower(k)] || strings.HasPrefix(strings.ToLower(k), "utm_") {
				delete(q, k)
			}
		}
		u.RawQuery = q.Encode() // Encode sorts by key
	}
	u.ForceQuery = false
	return u.String(), nil
}

// Text applies NFKC normalisation and collapses runs of white space into a
// single space, trimming both ends. It is used before fingerprinting so that
// formatting-only changes do not count as content changes.
func Text(s string) string {
	s = norm.NFKC.String(s)
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// Hash returns the hex SHA-256 of s.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// windowsReserved are device names Windows refuses as file names, with or
// without an extension.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func isUnsafeRune(r rune) bool {
	return r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r)
}

// CheckFileName reports why name cannot be used as a single path element on
// Windows (and other systems), or nil if it can.
func CheckFileName(name string) error {
	if name == "" {
		return errors.New("must not be empty")
	}
	if name == "." || name == ".." {
		return errors.New(`must not be "." or ".."`)
	}
	for _, r := range name {
		if isUnsafeRune(r) {
			return fmt.Errorf("must not contain %q", r)
		}
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return errors.New("must not end with a dot or space")
	}
	base := strings.ToUpper(name)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if windowsReserved[strings.TrimSpace(base)] {
		return fmt.Errorf("%q is a reserved name on Windows", name)
	}
	return nil
}

// Slug turns title into a file-name-safe string of at most maxRunes
// characters: letters and digits (any script) are kept, everything else
// becomes a single hyphen. The result may be empty.
func Slug(title string, maxRunes int) string {
	title = norm.NFKC.String(title)
	var b strings.Builder
	n := 0
	pendingHyphen := false
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) {
			if pendingHyphen && n > 0 {
				if n+1 >= maxRunes {
					break
				}
				b.WriteByte('-')
				n++
			}
			pendingHyphen = false
			if n >= maxRunes {
				break
			}
			b.WriteRune(unicode.ToLower(r))
			n++
		} else {
			pendingHyphen = true
		}
	}
	return b.String()
}

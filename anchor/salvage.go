package anchor

import (
	"regexp"
	"strconv"
	"strings"
)

// Anchors do not always publish valid TOML.
//
// On 2026-08-04 the stellar.toml served by ngnc.online — a production mainnet
// anchor, and the only issuer of NGNC — contained this line:
//
//	image="https://.../65f06c75052c9d3cf7bed94b_KESc.png" s
//
// The trailing "s" is not valid TOML, and a conforming parser rejects the
// entire document. That single stray character makes the most important
// anchor in this corridor invisible to any strictly-parsing client.
//
// Failing hard would be defensible as spec compliance and useless in
// practice: the anchor's capabilities are still plainly readable, and
// refusing to read them helps nobody. Silently accepting the file would be
// worse, because a malformed published document is itself a signal about how
// carefully an anchor is operated.
//
// So a strict parse is attempted first, and only on failure does the salvage
// pass below run — recovering the fields this package needs while recording
// that the document was malformed, so the defect is reported rather than
// hidden.

// keyValue matches a top-level TOML assignment, ignoring whatever follows the
// value. Tolerating trailing junk is the entire point.
var keyValue = regexp.MustCompile(`^\s*([A-Za-z0-9_]+)\s*=\s*(.+?)\s*$`)

// salvageTOML extracts recognised fields from a document a strict parser
// rejected. It never returns an error: a salvage pass that cannot find
// anything simply yields an empty TOML, and the caller reports the original
// parse failure.
func salvageTOML(body string) TOML {
	var t TOML
	var current *Currency

	// flush stores the currency block being accumulated.
	flush := func() {
		if current != nil {
			t.Currencies = append(t.Currencies, *current)
			current = nil
		}
	}

	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// A new [[CURRENCIES]] block, or any other section header, ends
		// whatever block was being accumulated.
		if strings.HasPrefix(line, "[") {
			flush()
			if strings.HasPrefix(line, "[[CURRENCIES]]") {
				current = &Currency{}
			}
			continue
		}

		m := keyValue.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := m[1], unquote(m[2])

		if current != nil {
			switch key {
			case "code":
				current.Code = val
			case "issuer":
				current.Issuer = val
			case "status":
				current.Status = val
			case "anchor_asset":
				current.AnchorAsset = val
			case "desc":
				current.Desc = val
			case "is_asset_anchored":
				current.IsAssetAnchored, _ = strconv.ParseBool(val)
			}
			continue
		}

		switch key {
		case "VERSION":
			t.Version = val
		case "NETWORK_PASSPHRASE":
			t.NetworkPassphrase = val
		case "WEB_AUTH_ENDPOINT":
			t.WebAuthEndpoint = val
		case "TRANSFER_SERVER":
			t.TransferServer = val
		case "TRANSFER_SERVER_SEP0024":
			t.TransferServer24 = val
		case "DIRECT_PAYMENT_SERVER":
			t.DirectPaymentServer = val
		case "ANCHOR_QUOTE_SERVER":
			t.AnchorQuoteServer = val
		case "KYC_SERVER":
			t.KYCServer = val
		case "SIGNING_KEY":
			t.SigningKey = val
		case "ORG_NAME":
			t.OrgName = val
		case "ORG_URL":
			t.OrgURL = val
		}
	}
	flush()
	return t
}

// unquote strips a leading quoted string from a raw TOML value, discarding
// any trailing content.
//
// Discarding rather than rejecting is deliberate: the trailing content is
// precisely the malformation being worked around. A value with no opening
// quote is returned trimmed, which handles bare booleans and numbers.
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) == 0 {
		return v
	}
	q := v[0]
	if q != '"' && q != '\'' {
		// Bare value; cut an inline comment if present.
		if i := strings.Index(v, "#"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		return v
	}
	// Find the closing quote, honouring backslash escapes for '"'.
	for i := 1; i < len(v); i++ {
		if v[i] == '\\' {
			i++
			continue
		}
		if v[i] == q {
			s := v[1:i]
			if q == '"' {
				// Only the escapes that plausibly appear in a
				// stellar.toml are handled; this is a recovery
				// path, not a general TOML implementation.
				s = strings.ReplaceAll(s, `\"`, `"`)
				s = strings.ReplaceAll(s, `\\`, `\`)
			}
			return s
		}
	}
	// Unterminated quote: take the rest of the line.
	return v[1:]
}

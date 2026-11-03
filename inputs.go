package smstunnel

import (
	"net/url"
	"strings"
	"unicode"
)

// The checks done before anything is sent, with the same rules as SMSTunnel's server.

const maxIdempotencyKeyLength = 255

func checkAPIKey(apiKey string) (string, error) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return "", invalid("an API key is required: create one in your SMSTunnel dashboard, Apps → Add an app (https://dashboard.smstunnel.io/apps)")
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '!' || key[i] > '~' {
			return "", invalid("the API key has characters an API key never has: copy it again from the dashboard")
		}
	}
	return key, nil
}

func checkBaseURL(rawURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", invalid("the base URL must be an absolute http or https address, such as " + DefaultBaseURL)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", invalid("the base URL must not have a query, a fragment or a user name")
	}
	base := u.Scheme + "://" + u.Host + u.EscapedPath()
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	return base, nil
}

// checkCountryCode turns "40", "+40", "0040" or "+ 40" into "+40", and "" into "". Anything else is refused:
// SMSTunnel would skip it and complete the number with another country's code.
func checkCountryCode(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	code := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' || r == '(' || r == ')' || r == '.' {
			return -1
		}
		return r
	}, value)
	switch {
	case strings.HasPrefix(code, "00"):
		code = "+" + code[2:]
	case !strings.HasPrefix(code, "+"):
		code = "+" + code
	}
	valid := len(code) >= 2 && len(code) <= 5 && code[1] >= '1' && code[1] <= '9'
	for i := 2; valid && i < len(code); i++ {
		valid = code[i] >= '0' && code[i] <= '9'
	}
	if !valid {
		return "", invalid(`"` + value + `" is not a country code: give 1 to 4 digits, with or without + or 00, for example "+40" or "49"`)
	}
	return code, nil
}

func checkIdempotencyKey(key string) error {
	if key == "" {
		return nil
	}
	ok := len(key) <= maxIdempotencyKeyLength && strings.TrimSpace(key) != ""
	for i := 0; ok && i < len(key); i++ {
		ok = key[i] >= ' ' && key[i] <= '~'
	}
	if !ok {
		return invalid(`the idempotency key must be 1 to 255 printable ASCII characters, for example a UUID or "order-1042-shipped"`)
	}
	return nil
}

func checkRecipient(to string) error {
	if strings.TrimSpace(to) == "" {
		return invalid("give the recipient's number, in international format such as +14155550123")
	}
	if strings.ContainsAny(to, ",;") {
		return invalid("give one number per text: to text several people, send once per number")
	}
	return nil
}

func checkText(message string) error {
	if strings.TrimSpace(message) == "" {
		return invalid("the text is empty")
	}
	return nil
}

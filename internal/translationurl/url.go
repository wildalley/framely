// Package translationurl shares the transport policy for translation settings
// and requests: HTTPS for remote services, HTTP only for local inference.
package translationurl

import (
	"errors"
	"net/netip"
	"net/url"
	"strings"
)

func Validate(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("翻译接口地址必须是完整的 HTTPS 地址；本机服务可使用 HTTP")
	}
	return ValidateURL(u)
}

func ValidateURL(u *url.URL) error {
	if u == nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return errors.New("翻译接口地址必须是完整的 HTTPS 地址；本机服务可使用 HTTP")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
		if host == "localhost" {
			return nil
		}
		if addr, err := netip.ParseAddr(host); err == nil && addr.Unmap().IsLoopback() {
			return nil
		}
	}
	return errors.New("远程翻译接口必须使用 HTTPS，以保护密钥和文字；HTTP 仅限本机服务")
}

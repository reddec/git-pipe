package embedded

import (
	"net"
	"net/http"
	"strings"
)

// RequestResolver defines logic how to match incoming request and domain.
type RequestResolver interface {
	// FQDN version of domain name.
	FQDN(domain string) string
	// Domain name from request.
	Domain(req *http.Request) string
}

// ByDomain resolves request domain based on requested host.
// Root domain (if defined) will be used as parent domain for all records.
func ByDomain(rootDomain string) RequestResolver {
	return &domainResolver{rootDomain: rootDomain}
}

// ByRoot is convenient alias to ByDomain without root domain: assumes all records domains already in FQDN form.
func ByRoot() RequestResolver {
	return ByDomain("")
}

type domainResolver struct {
	rootDomain string
}

func (dr *domainResolver) FQDN(domain string) string {
	if dr.rootDomain != "" {
		return domain + "." + dr.rootDomain
	}
	return domain
}

func (dr *domainResolver) Domain(request *http.Request) string {
	domain, _, _ := net.SplitHostPort(request.Host)
	if domain == "" {
		domain = request.Host
	}
	return domain
}

// ByPath resolve request domain as a first segment in request URL path.
// It modifies request URL in case of successful resolution.
func ByPath() RequestResolver {
	return &pathResolver{}
}

type pathResolver struct{}

func (pr *pathResolver) FQDN(domain string) string {
	return domain
}

func (pr *pathResolver) Domain(request *http.Request) string {
	for _, path := range strings.SplitN(request.URL.Path, "/", 3) { //nolint:gomnd
		if len(path) != 0 {
			request.URL.Path = request.URL.Path[len(path)+1:]
			return path
		}
	}
	return ""
}

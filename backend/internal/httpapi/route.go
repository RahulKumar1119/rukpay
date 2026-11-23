package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"
)

// The project targets Go 1.21.1, whose http.ServeMux has no method or {id}
// patterns (those arrived in Go 1.22). This file holds a small dispatcher that
// matches a method and a path made of literal segments and one wildcard
// segment, {id} (Requirement 10.1).

// idWildcard is the path segment that matches any one non-empty segment.
const idWildcard = "{id}"

type ctxKey int

const (
	idKey ctxKey = iota
	infoKey
)

// vaultIDPattern is the shape of a vault id: 32 lower-case hex characters. Only
// an id of this shape is ever written to a log line, so a path or query value
// chosen by a caller cannot put its own text in the log.
var vaultIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// requestInfo is what the logging middleware reports about one request. The
// router and the handlers fill it in as they learn it.
type requestInfo struct {
	route   string // method and pattern, for example "GET /api/v1/vault/{id}"
	vaultID string
}

// setVaultID records id for the log line when it has the shape of a vault id.
func (i *requestInfo) setVaultID(id string) {
	if i != nil && vaultIDPattern.MatchString(id) {
		i.vaultID = id
	}
}

func infoFrom(r *http.Request) *requestInfo {
	info, _ := r.Context().Value(infoKey).(*requestInfo)
	return info
}

// idFrom returns the {id} segment of the matched route.
func idFrom(r *http.Request) string {
	id, _ := r.Context().Value(idKey).(string)
	return id
}

type route struct {
	method   string
	pattern  string
	segments []string
	handler  http.HandlerFunc
}

// dispatcher matches a request to a route.
type dispatcher struct {
	routes []route
}

// handle registers handler for the method and path pattern.
func (d *dispatcher) handle(method, pattern string, handler http.HandlerFunc) {
	d.routes = append(d.routes, route{
		method:   method,
		pattern:  pattern,
		segments: splitPath(pattern),
		handler:  handler,
	})
}

// splitPath splits a path into segments. A trailing slash leaves an empty last
// segment, so "/a/b/" does not match the pattern "/a/b".
func splitPath(path string) []string {
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

// match reports whether the path segments fit the route and returns the value
// of the {id} segment.
func (rt route) match(segments []string) (id string, ok bool) {
	if len(segments) != len(rt.segments) {
		return "", false
	}
	for i, want := range rt.segments {
		got := segments[i]
		if want == idWildcard {
			if got == "" {
				return "", false
			}
			id = got
			continue
		}
		if got != want {
			return "", false
		}
	}
	return id, true
}

// ServeHTTP runs the first route that fits the method and the path. A path that
// fits a route under another method gets 405 and an Allow header. Any other
// path gets 404.
func (d *dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	segments := splitPath(r.URL.Path)

	var allow []string
	for _, rt := range d.routes {
		id, ok := rt.match(segments)
		if !ok {
			continue
		}
		if rt.method != r.Method {
			allow = append(allow, rt.method)
			continue
		}

		if info := infoFrom(r); info != nil {
			info.route = rt.method + " " + rt.pattern
			info.setVaultID(id)
		}
		ctx := context.WithValue(r.Context(), idKey, id)
		rt.handler(w, r.WithContext(ctx))
		return
	}

	if len(allow) > 0 {
		w.Header().Set("Allow", strings.Join(allow, ", "))
		writeCode(w, http.StatusMethodNotAllowed, codeMethodNotAllowed, msgMethodNotAllowed)
		return
	}
	writeCode(w, http.StatusNotFound, codeNotFound, msgNotFound)
}

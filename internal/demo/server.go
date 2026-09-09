package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/idoavrah/entra-tui/internal/auth"
	"github.com/idoavrah/entra-tui/internal/graph"
)

// baseURL is the address the in-process server answers on. Nothing ever
// reaches a network, but the client still needs a well-formed URL.
const baseURL = "https://demo.invalid/v1.0"

// directoryObjectProperties are the only properties a heterogeneous
// relationship declares.
//
// memberOf, members and owners are collections of directoryObject, and Graph
// rejects a $select naming a property that type does not have -- so asking
// for groupTypes or userPrincipalName there fails the whole request. The
// stand-in enforces that, because a fake which quietly ignores $select lets
// exactly this bug through to a real tenant.
var directoryObjectProperties = map[string]bool{
	"id": true, "displayName": true, "deletedDateTime": true,
}

// Server is an in-process Microsoft Graph stand-in over generated data.
type Server struct {
	mu   sync.Mutex
	data *Data
	// properties records which properties each collection declares.
	properties map[string]map[string]bool
}

// NewServer builds a stand-in over a freshly generated directory.
func NewServer(seed int64) *Server {
	d := Generate(seed)
	s := &Server{data: d, properties: map[string]map[string]bool{}}
	for name, items := range s.collections() {
		s.properties[name] = declaredProperties(items)
	}
	return s
}

// Data exposes the generated directory, for tests that want to assert
// against it directly.
func (s *Server) Data() *Data { return s.data }

// Client returns a Graph client wired straight to this server.
func (s *Server) Client() *graph.Client {
	return graph.New(staticToken{},
		graph.WithBaseURL(baseURL),
		graph.WithHTTPClient(&http.Client{Transport: inProcess{handler: s}}))
}

// Identity is the fictional signed-in account.
func (s *Server) Identity() auth.Identity {
	return auth.Identity{
		Account:  "kaelen.stormrider0@" + Tenant + ".onmicrosoft.com",
		Name:     "Kaelen Stormrider",
		TenantID: "aetherforge-demo-tenant",
		Method:   "demo",
	}
}

func (s *Server) collections() map[string][]graph.Item {
	return map[string][]graph.Item{
		"users":             s.data.Users,
		"groups":            s.data.Groups,
		"applications":      s.data.Applications,
		"servicePrincipals": s.data.ServicePrincipals,
		"devices":           s.data.Devices,
	}
}

// declaredProperties is the union of the keys a collection's objects carry.
func declaredProperties(items []graph.Item) map[string]bool {
	out := map[string]bool{"id": true}
	for _, it := range items {
		for k := range it {
			out[k] = true
		}
	}
	return out
}

// ---------------------------------------------------------------- routing

var (
	countPath   = regexp.MustCompile(`^/v1\.0/(\w+)/\$count$`)
	refPath     = regexp.MustCompile(`^/v1\.0/(\w+)/([\w-]+)/(\w+)/\$ref$`)
	unrefPath   = regexp.MustCompile(`^/v1\.0/(\w+)/([\w-]+)/(\w+)/([\w-]+)/\$ref$`)
	relPath     = regexp.MustCompile(`^/v1\.0/(\w+)/([\w-]+)/(\w+)$`)
	objectPath  = regexp.MustCompile(`^/v1\.0/(\w+)/([\w-]+)$`)
	listPath    = regexp.MustCompile(`^/v1\.0/(\w+)$`)
	filterByApp = regexp.MustCompile(`^appId eq '([^']*)'$`)
)

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && refPath.MatchString(path):
		m := refPath.FindStringSubmatch(path)
		s.addRef(w, r, m[2], m[3])
	case r.Method == http.MethodDelete && unrefPath.MatchString(path):
		m := unrefPath.FindStringSubmatch(path)
		s.removeRef(w, m[2], m[3], m[4])
	case countPath.MatchString(path):
		s.count(w, r, countPath.FindStringSubmatch(path)[1])
	case relPath.MatchString(path):
		m := relPath.FindStringSubmatch(path)
		s.relationship(w, r, m[2], m[3])
	case objectPath.MatchString(path):
		m := objectPath.FindStringSubmatch(path)
		s.object(w, r, m[1], m[2])
	case listPath.MatchString(path):
		s.list(w, r, listPath.FindStringSubmatch(path)[1])
	default:
		writeError(w, http.StatusNotFound, "Request_ResourceNotFound", "no such resource")
	}
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, collection string) {
	items, ok := s.collections()[collection]
	if !ok {
		writeError(w, http.StatusNotFound, "Request_ResourceNotFound", "no such collection")
		return
	}
	q := r.URL.Query()

	if bad := s.checkSelect(q, s.properties[collection]); bad != "" {
		writeUnknownProperty(w, bad)
		return
	}
	if (q.Has("$count") || q.Has("$search")) && r.Header.Get("ConsistencyLevel") != "eventual" {
		writeError(w, http.StatusBadRequest, "Request_UnsupportedQuery",
			"Request with $count or $search requires ConsistencyLevel: eventual.")
		return
	}

	if expr := q.Get("$filter"); expr != "" {
		m := filterByApp.FindStringSubmatch(expr)
		if m == nil {
			writeError(w, http.StatusBadRequest, "Request_UnsupportedQuery", "unsupported $filter")
			return
		}
		items = filter(items, func(it graph.Item) bool { return it.String("appId") == m[1] })
	}
	if term := searchTerm(q.Get("$search")); term != "" {
		items = filter(items, func(it graph.Item) bool { return matches(it, term) })
	}

	sorted := append([]graph.Item(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].String("displayName")) < strings.ToLower(sorted[j].String("displayName"))
	})

	top := intParam(q, "$top", 100)
	skip := intParam(q, "$skip", 0)
	page := sorted
	if skip < len(page) {
		page = page[skip:]
	} else {
		page = nil
	}
	if len(page) > top {
		page = page[:top]
	}

	body := map[string]any{"value": project(page, q.Get("$select"))}
	if q.Has("$count") {
		body["@odata.count"] = len(sorted)
	}
	if skip+top < len(sorted) {
		next := *r.URL
		values := next.Query()
		values.Set("$skip", strconv.Itoa(skip+top))
		next.RawQuery = values.Encode()
		body["@odata.nextLink"] = baseURL + strings.TrimPrefix(next.Path, "/v1.0") + "?" + next.RawQuery
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) object(w http.ResponseWriter, r *http.Request, collection, id string) {
	for _, it := range s.collections()[collection] {
		if it.ID() == id {
			out := graph.Item{}
			for k, v := range it {
				out[k] = v
			}
			out["@odata.context"] = baseURL + "/$metadata#" + collection
			writeJSON(w, http.StatusOK, out)
			return
		}
	}
	writeError(w, http.StatusNotFound, "Request_ResourceNotFound", "no such object")
}

func (s *Server) count(w http.ResponseWriter, r *http.Request, collection string) {
	if r.Header.Get("ConsistencyLevel") != "eventual" {
		writeError(w, http.StatusBadRequest, "Request_UnsupportedQuery",
			"$count requires ConsistencyLevel: eventual.")
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, len(s.collections()[collection]))
}

func (s *Server) relationship(w http.ResponseWriter, r *http.Request, id, rel string) {
	var items []graph.Item
	switch rel {
	case "owners", "registeredOwners":
		items = s.data.Owners[id]
	case "members":
		items = s.data.Members[id]
	case "memberOf":
		items = s.data.MemberOf[id]
	case "appRoleAssignedTo":
		items = s.data.Assignments[id]
	case "oauth2PermissionGrants":
		items = grantItems(s.data.GrantedScopes[id])
	case "appRoleAssignments":
		items = roleItems(s.data.GrantedRoles[id])
	default:
		writeError(w, http.StatusNotFound, "Request_ResourceNotFound", "no such relationship")
		return
	}

	// The heterogeneous relationships are typed as directoryObject, so only
	// its properties may be selected -- and every object in one carries the
	// @odata.type saying what it actually is.
	if isHeterogeneous(rel) {
		if bad := s.checkSelect(r.URL.Query(), directoryObjectProperties); bad != "" {
			writeUnknownProperty(w, bad)
			return
		}
		items = s.annotate(items)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"value": project(items, r.URL.Query().Get("$select")),
	})
}

// annotate stamps each object with its Graph type. A caller reading members
// or owners has nothing else to go on: the collection is declared as
// directoryObject, so the properties that would give an object away are not
// even selectable.
func (s *Server) annotate(items []graph.Item) []graph.Item {
	out := make([]graph.Item, 0, len(items))
	for _, it := range items {
		copied := maps.Clone(it)
		if t := s.data.ODataType[it.ID()]; t != "" {
			copied["@odata.type"] = t
		}
		out = append(out, copied)
	}
	return out
}

func isHeterogeneous(rel string) bool {
	switch rel {
	case "owners", "registeredOwners", "members", "memberOf":
		return true
	}
	return false
}

func (s *Server) addRef(w http.ResponseWriter, r *http.Request, id, rel string) {
	body, _ := io.ReadAll(r.Body)
	var ref struct {
		ODataID string `json:"@odata.id"`
	}
	if err := json.Unmarshal(body, &ref); err != nil || ref.ODataID == "" {
		writeError(w, http.StatusBadRequest, "Request_BadRequest", "missing @odata.id")
		return
	}

	principalID := ref.ODataID[strings.LastIndex(ref.ODataID, "/")+1:]
	object := s.find(principalID)
	if object == nil {
		writeError(w, http.StatusNotFound, "Request_ResourceNotFound", "no such principal")
		return
	}

	target := &s.data.Owners
	if rel == "members" {
		target = &s.data.Members
	}
	for _, existing := range (*target)[id] {
		if existing.ID() == principalID {
			writeError(w, http.StatusBadRequest, "Request_BadRequest",
				"One or more added object references already exist.")
			return
		}
	}
	(*target)[id] = append((*target)[id], object)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeRef(w http.ResponseWriter, id, rel, principalID string) {
	target := &s.data.Owners
	if rel == "members" {
		target = &s.data.Members
	}
	for i, existing := range (*target)[id] {
		if existing.ID() == principalID {
			(*target)[id] = append((*target)[id][:i], (*target)[id][i+1:]...)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	writeError(w, http.StatusNotFound, "Request_ResourceNotFound", "not a member")
}

// find locates any object by id, across every collection.
func (s *Server) find(id string) graph.Item {
	for _, items := range s.collections() {
		for _, it := range items {
			if it.ID() == id {
				return it
			}
		}
	}
	return nil
}

// checkSelect returns the first selected property the collection does not
// declare, or "" when every one is valid.
func (s *Server) checkSelect(q url.Values, declared map[string]bool) string {
	if declared == nil {
		return ""
	}
	for _, property := range strings.Split(q.Get("$select"), ",") {
		property = strings.TrimSpace(property)
		if property != "" && !declared[property] {
			return property
		}
	}
	return ""
}

// ----------------------------------------------------------------- helpers

func project(items []graph.Item, selection string) []graph.Item {
	fields := strings.FieldsFunc(selection, func(r rune) bool { return r == ',' })
	out := make([]graph.Item, 0, len(items))
	for _, it := range items {
		if len(fields) == 0 {
			out = append(out, it)
			continue
		}
		projected := graph.Item{}
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if v, ok := it[f]; ok {
				projected[f] = v
			}
		}
		if t, ok := it["@odata.type"]; ok {
			projected["@odata.type"] = t
		}
		out = append(out, projected)
	}
	return out
}

func filter(items []graph.Item, keep func(graph.Item) bool) []graph.Item {
	out := make([]graph.Item, 0, len(items))
	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}
	return out
}

// searchTerm pulls the term out of Graph's `"field:term" OR "field:term"`
// search syntax.
func searchTerm(expr string) string {
	if expr == "" {
		return ""
	}
	first := strings.SplitN(expr, " OR ", 2)[0]
	first = strings.Trim(first, `"`)
	if i := strings.Index(first, ":"); i >= 0 {
		return strings.ToLower(first[i+1:])
	}
	return strings.ToLower(first)
}

func matches(it graph.Item, term string) bool {
	for _, key := range []string{"displayName", "userPrincipalName", "mail", "description"} {
		if strings.Contains(strings.ToLower(it.String(key)), term) {
			return true
		}
	}
	return false
}

func intParam(q url.Values, key string, fallback int) int {
	if v, err := strconv.Atoi(q.Get(key)); err == nil {
		return v
	}
	return fallback
}

func grantItems(scopes []string) []graph.Item {
	if len(scopes) == 0 {
		return nil
	}
	return []graph.Item{{"id": "grant-1", "scope": strings.Join(scopes, " ")}}
}

func roleItems(roles []string) []graph.Item {
	out := make([]graph.Item, 0, len(roles))
	for i, id := range roles {
		out = append(out, graph.Item{"id": fmt.Sprintf("assignment-%d", i), "appRoleId": id})
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"code": code, "message": message},
	})
}

func writeUnknownProperty(w http.ResponseWriter, property string) {
	writeError(w, http.StatusBadRequest, "Request_UnsupportedQuery",
		fmt.Sprintf("Property '%s' does not exist as a declared property or extension property.", property))
}

// --------------------------------------------------------- in-process wiring

// inProcess dispatches requests straight to a handler, so the demo needs no
// listening socket and no port to collide with.
type inProcess struct{ handler http.Handler }

func (t inProcess) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := &recorder{header: http.Header{}, status: http.StatusOK}
	t.handler.ServeHTTP(rec, r)
	return &http.Response{
		StatusCode: rec.status,
		Header:     rec.header,
		Body:       io.NopCloser(bytes.NewReader(rec.body.Bytes())),
		Request:    r,
	}, nil
}

// recorder is a minimal ResponseWriter. net/http/httptest would do, but it
// drags test-only machinery into the shipped binary.
type recorder struct {
	header http.Header
	body   bytes.Buffer
	status int
	wrote  bool
}

func (r *recorder) Header() http.Header { return r.header }
func (r *recorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.body.Write(b)
}
func (r *recorder) WriteHeader(status int) {
	if !r.wrote {
		r.status = status
	}
}

// staticToken satisfies the token provider without any sign-in.
type staticToken struct{}

func (staticToken) Token(context.Context) (string, error) { return "demo-token", nil }
func (staticToken) Identity() auth.Identity {
	return auth.Identity{Account: "demo", Method: "demo"}
}

// Package demo provides a fictional directory and an in-process Microsoft
// Graph stand-in, so entra-tui can be run, screenshotted and tested without
// a tenant.
package demo

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/idoavrah/entra-tui/internal/graph"
)

// Sizes of the generated directory. Large enough that paging, sorting and
// searching all do something visible.
const (
	UserCount   = 420
	GroupCount  = 130
	AppCount    = 85
	DeviceCount = 160
	OwnerCount  = 3
	// OwnerlessInN is how often a group or application is generated with no
	// owners at all. Ownerless objects are a real and common shape -- one
	// created by an admin who has since left, or synced from on-premises --
	// and a directory browser has to show that emptiness convincingly.
	OwnerlessInN = 6
	MemberCount  = 12
	MemberOfSize = 4
)

// GraphAppID is Microsoft Graph's own application id, used as the resource
// for the generated API permissions.
const GraphAppID = "00000003-0000-0000-c000-000000000000"

var (
	// The fiction is a game studio's directory: Aetherforge Interactive,
	// which ships a handful of invented titles. Everything here is made up,
	// so nothing resembles a real person, company or product.
	givenNames = []string{
		"Kaelen", "Sylvara", "Draxin", "Orinthe", "Vesper", "Thorne", "Nyx",
		"Cassia", "Bram", "Sable", "Quill", "Riven", "Marek", "Ilyana",
		"Corvin", "Elowen", "Fenris", "Isolde", "Jorund", "Lyra", "Osric",
		"Perrin", "Rowan", "Tamsin", "Ulric", "Wren", "Yrsa", "Zephyr",
	}
	surnames = []string{
		"Stormrider", "Emberfall", "Nightwood", "Ashvale", "Ironhollow",
		"Duskbane", "Windmere", "Frostmoor", "Thornquist", "Grimsend",
		"Ravenholt", "Silverbrook", "Hollowmere", "Blackwater", "Gildenmoor",
		"Thistlewood", "Marrowvale", "Stonefen",
	}
	// Hebrew names, so right-to-left rendering is exercised by default.
	hebrewGiven   = []string{"שרה", "דוד", "רחל", "יוסף", "מרים", "אבנר"}
	hebrewSurname = []string{"כהן", "לוי", "מזרחי", "פרץ", "ביטון", "אזולאי"}

	// Studio teams rather than corporate departments.
	departments = []string{
		"Engine", "Gameplay", "Live Ops", "Narrative", "Art & Animation",
		"Quality Assurance", "Player Support", "Publishing", "Audio",
	}
	jobTitles = []string{
		"Gameplay Engineer", "Technical Artist", "Level Designer",
		"Narrative Designer", "Producer", "QA Analyst", "Community Manager",
		"Engine Programmer", "Concept Artist",
	}
	// Invented places, one studio site each.
	offices = []string{"Aetherford", "Ravenspire", "Solmarch", "Duskhaven", "Emberkeep"}

	// The studio's titles, used to name groups and backend services.
	titles = []string{
		"Shattered Crown", "Neon Requiem", "Starfall Tactics",
		"Hollow Tide", "Ember & Ash", "Verdant Reach",
	}
	// Backend services a game studio actually runs.
	services = []string{
		"Matchmaking", "Player Telemetry", "Leaderboards", "Anti-Cheat Sentinel",
		"Store Backend", "Guild Chat Relay", "Save Sync", "Build Pipeline",
		"Crash Triage", "Live Events",
	}
	// What a group of people at a studio gets called.
	guildKinds = []string{"Guild", "Strike Team", "Council", "Squad", "Crew"}

	// Workstations and console devkits.
	operatingSystems = []struct{ name, version, maker, model string }{
		{"Windows", "10.0.22631", "Forgeworks", "Anvil WS-9"},
		{"macOS", "14.5", "Orchard", "Atelier 16"},
		{"Linux", "6.8.0", "Forgeworks", "Anvil Render Node"},
		{"Android", "14", "Kestrel", "Handheld Devkit"},
		{"Windows", "10.0.19045", "Forgeworks", "Anvil WS-7"},
	}

	graphPermissions = []struct {
		id, value, description string
		application            bool
	}{
		{"df021288-bdef-4463-88db-98f22de89214", "User.Read.All", "Read all users' full profiles", true},
		{"b340eb25-3456-403f-be2f-af7a0d370277", "User.ReadBasic.All", "Read all users' basic profiles", false},
		{"5b567255-7703-4780-807c-7be8301ae99b", "Group.Read.All", "Read all groups", true},
		{"7ab1d382-f21e-4acd-a863-ba3e13f7da61", "Directory.Read.All", "Read directory data", true},
		{"e1fe6dd8-ba31-4d61-89e7-88639da4683d", "User.Read", "Sign in and read user profile", false},
		{"06da0dbc-49e2-44d2-8312-53f166ab848a", "Directory.AccessAsUser.All", "Access the directory as the signed-in user", false},
	}
)

// Tenant is the fictional studio's domain.
const Tenant = "aetherforge"

// Data is a generated directory.
type Data struct {
	Users             []graph.Item
	Groups            []graph.Item
	Applications      []graph.Item
	ServicePrincipals []graph.Item
	Devices           []graph.Item

	// Relationships, keyed by the owning object's id.
	Owners      map[string][]graph.Item
	Members     map[string][]graph.Item
	MemberOf    map[string][]graph.Item
	Assignments map[string][]graph.Item

	// GrantedScopes and GrantedRoles model consent on a service principal.
	GrantedScopes map[string][]string
	GrantedRoles  map[string][]string
}

// Generate builds a deterministic directory. The same seed always produces
// the same objects, which is what lets screenshots be compared between runs.
func Generate(seed int64) *Data {
	r := rand.New(rand.NewSource(seed))
	d := &Data{
		Owners:        map[string][]graph.Item{},
		Members:       map[string][]graph.Item{},
		MemberOf:      map[string][]graph.Item{},
		Assignments:   map[string][]graph.Item{},
		GrantedScopes: map[string][]string{},
		GrantedRoles:  map[string][]string{},
	}

	d.Users = generateUsers(r)
	d.Groups = generateGroups(r)
	d.Applications = generateApplications(r)
	d.ServicePrincipals = generateServicePrincipals(d.Applications)
	d.Devices = generateDevices(r)
	d.linkRelationships(r)
	return d
}

func generateUsers(r *rand.Rand) []graph.Item {
	out := make([]graph.Item, 0, UserCount)
	for i := range UserCount {
		name, login := personName(r, i)
		created := someTimeWithin(r, 5*365)
		out = append(out, graph.Item{
			"id":                    fmt.Sprintf("u%04d", i),
			"displayName":           name,
			"userPrincipalName":     login + "@" + Tenant + ".onmicrosoft.com",
			"mail":                  login + "@" + Tenant + ".games",
			"userType":              pick(r, []string{"Member", "Member", "Member", "Member", "Guest"}),
			"accountEnabled":        r.Intn(9) != 0,
			"jobTitle":              pick(r, jobTitles),
			"department":            pick(r, departments),
			"officeLocation":        pick(r, offices),
			"mobilePhone":           fmt.Sprintf("+44 20 7946 %04d", r.Intn(10000)),
			"createdDateTime":       created,
			"onPremisesSyncEnabled": r.Intn(3) == 0,
			"usageLocation":         pick(r, []string{"GB", "IL", "US", "IE", "IN"}),
			"employeeId":            fmt.Sprintf("E%05d", 1000+i),
		})
	}
	return out
}

func generateGroups(r *rand.Rand) []graph.Item {
	out := make([]graph.Item, 0, GroupCount)
	for i := range GroupCount {
		unified := i%3 == 0
		dynamic := i%7 == 0

		name := fmt.Sprintf("%s %s", pick(r, departments), pick(r, guildKinds))
		if i%5 == 2 {
			name = fmt.Sprintf("%s %s", pick(r, titles), pick(r, guildKinds))
		}
		if i%11 == 4 {
			name = pick(r, hebrewGiven) + " צוות"
		}

		types := []any{}
		if unified {
			types = append(types, "Unified")
		}
		if dynamic {
			types = append(types, "DynamicMembership")
		}

		g := graph.Item{
			"id":                    fmt.Sprintf("g%04d", i),
			"displayName":           fmt.Sprintf("%s %d", name, i),
			"description":           "Ships and operates " + pick(r, titles),
			"mailEnabled":           unified,
			"securityEnabled":       !unified || i%2 == 0,
			"groupTypes":            types,
			"visibility":            pick(r, []string{"Private", "Public"}),
			"createdDateTime":       someTimeWithin(r, 4*365),
			"onPremisesSyncEnabled": r.Intn(4) == 0,
			"isAssignableToRole":    i%13 == 0,
		}
		if unified {
			g["mail"] = fmt.Sprintf("team%d@%s.games", i, Tenant)
			g["mailNickname"] = fmt.Sprintf("team%d", i)
		}
		if dynamic {
			g["membershipRule"] = `user.department -eq "` + pick(r, departments) + `"`
			g["membershipRuleProcessingState"] = "On"
		}
		out = append(out, g)
	}
	return out
}

func generateApplications(r *rand.Rand) []graph.Item {
	out := make([]graph.Item, 0, AppCount)
	for i := range AppCount {
		appID := fmt.Sprintf("%08d-1111-2222-3333-444444444444", i)

		var secrets, certs []any
		if i%2 == 0 {
			secrets = append(secrets, map[string]any{
				"displayName": "production", "endDateTime": someTimeAhead(r, 400),
			})
		}
		if i%5 == 0 {
			// Some have already lapsed, so the warning colour has something
			// to say.
			certs = append(certs, map[string]any{
				"displayName": "signing", "endDateTime": someTimeWithin(r, 200),
			})
		}

		access := []any{}
		for _, p := range graphPermissions {
			if r.Intn(3) == 0 {
				kind := "Scope"
				if p.application {
					kind = "Role"
				}
				access = append(access, map[string]any{"id": p.id, "type": kind})
			}
		}

		app := graph.Item{
			"id":              fmt.Sprintf("a%04d", i),
			"appId":           appID,
			"displayName":     fmt.Sprintf("%s — %s", pick(r, titles), pick(r, services)),
			"signInAudience":  pick(r, []string{"AzureADMyOrg", "AzureADMultipleOrgs", "AzureADandPersonalMicrosoftAccount"}),
			"publisherDomain": Tenant + ".games",
			"createdDateTime": someTimeWithin(r, 6*365),
			"identifierUris":  []any{fmt.Sprintf("api://%s/%d", Tenant, i)},
			"web": map[string]any{
				"redirectUris": []any{fmt.Sprintf("https://svc%d.%s.games/callback", i, Tenant)},
				"homePageUrl":  fmt.Sprintf("https://svc%d.%s.games", i, Tenant),
				"implicitGrantSettings": map[string]any{
					"enableAccessTokenIssuance": i%9 == 0,
					"enableIdTokenIssuance":     i%2 == 0,
				},
			},
			"passwordCredentials":    secrets,
			"keyCredentials":         certs,
			"requiredResourceAccess": []any{map[string]any{"resourceAppId": GraphAppID, "resourceAccess": access}},
		}
		if i%3 == 0 {
			app["spa"] = map[string]any{"redirectUris": []any{fmt.Sprintf("https://play%d.%s.games/", i, Tenant)}}
		}
		if i%4 == 0 {
			app["publicClient"] = map[string]any{"redirectUris": []any{"http://localhost"}}
		}
		if i%3 == 0 {
			app["appRoles"] = []any{map[string]any{
				"id": fmt.Sprintf("role-%d", i), "displayName": "Live Ops Admin", "value": "LiveOps.Admin",
				"allowedMemberTypes": []any{"User"}, "isEnabled": true,
			}}
		}
		out = append(out, app)
	}
	return out
}

// generateServicePrincipals mirrors the applications, plus the Microsoft
// Graph service principal that publishes the permission catalogue.
func generateServicePrincipals(apps []graph.Item) []graph.Item {
	out := make([]graph.Item, 0, len(apps)+1)
	for i, app := range apps {
		sp := graph.Item{
			"id":                        fmt.Sprintf("s%04d", i),
			"appId":                     app["appId"],
			"displayName":               app["displayName"],
			"accountEnabled":            i%9 != 0,
			"servicePrincipalType":      []string{"Application", "ManagedIdentity", "Legacy"}[i%3],
			"appRoleAssignmentRequired": i%2 == 0,
			"publisherName":             "Aetherforge Interactive",
			"homepage":                  fmt.Sprintf("https://svc%d.%s.games", i, Tenant),
			"replyUrls":                 []any{fmt.Sprintf("https://svc%d.%s.games/callback", i, Tenant)},
			"servicePrincipalNames":     []any{app["appId"]},
			"createdDateTime":           app["createdDateTime"],
			"signInAudience":            app["signInAudience"],
		}
		if mode := []string{"saml", "oidc", ""}[i%3]; mode != "" {
			sp["preferredSingleSignOnMode"] = mode
		}
		if roles, ok := app["appRoles"]; ok {
			sp["appRoles"] = roles
		}
		out = append(out, sp)
	}

	scopes := make([]any, 0)
	roles := make([]any, 0)
	for _, p := range graphPermissions {
		entry := map[string]any{
			"id": p.id, "value": p.value, "adminConsentDisplayName": p.description,
		}
		if p.application {
			entry["displayName"] = p.description
			entry["allowedMemberTypes"] = []any{"Application"}
			entry["isEnabled"] = true
			roles = append(roles, entry)
			continue
		}
		entry["type"] = "User"
		if p.value == "Directory.AccessAsUser.All" {
			entry["type"] = "Admin"
		}
		scopes = append(scopes, entry)
	}

	return append(out, graph.Item{
		"id": "sp-graph", "appId": GraphAppID, "displayName": "Microsoft Graph",
		"servicePrincipalType": "Application", "accountEnabled": true,
		"publisherName":          "Microsoft Services",
		"oauth2PermissionScopes": scopes,
		"appRoles":               roles,
	})
}

func generateDevices(r *rand.Rand) []graph.Item {
	out := make([]graph.Item, 0, DeviceCount)
	for i := range DeviceCount {
		os := operatingSystems[i%len(operatingSystems)]
		out = append(out, graph.Item{
			"id":                            fmt.Sprintf("d%04d", i),
			"deviceId":                      fmt.Sprintf("%08d-dddd-eeee-ffff-000000000000", i),
			"displayName":                   fmt.Sprintf("%s-%s-%04d", pick(r, []string{"FORGE", "ANVIL", "KILN"}), shortOS(os.name), 1000+i),
			"operatingSystem":               os.name,
			"operatingSystemVersion":        os.version,
			"manufacturer":                  os.maker,
			"model":                         os.model,
			"trustType":                     []string{"AzureAd", "ServerAd", "Workplace"}[i%3],
			"isCompliant":                   i%6 != 0,
			"isManaged":                     i%5 != 0,
			"accountEnabled":                i%17 != 0,
			"profileType":                   "RegisteredDevice",
			"enrollmentType":                "AzureDomainJoined",
			"registrationDateTime":          someTimeWithin(r, 3*365),
			"approximateLastSignInDateTime": someTimeWithin(r, 60),
			"onPremisesSyncEnabled":         i%3 == 0,
		})
	}
	return out
}

// linkRelationships wires owners, members and assignments between the
// generated objects.
func (d *Data) linkRelationships(r *rand.Rand) {
	principal := func() graph.Item { return d.Users[r.Intn(len(d.Users))] }

	// ownerless reports whether this object should be generated with none.
	ownerless := func() bool { return r.Intn(OwnerlessInN) == 0 }

	for _, g := range d.Groups {
		id := g.ID()
		if !ownerless() {
			for range OwnerCount {
				d.Owners[id] = append(d.Owners[id], principal())
			}
		}
		for range MemberCount + r.Intn(MemberCount) {
			d.Members[id] = append(d.Members[id], principal())
		}
		// A nested group, so the members list is genuinely heterogeneous.
		d.Members[id] = append(d.Members[id], d.Groups[r.Intn(len(d.Groups))])
	}

	for _, u := range d.Users {
		for range 1 + r.Intn(MemberOfSize) {
			d.MemberOf[u.ID()] = append(d.MemberOf[u.ID()], d.Groups[r.Intn(len(d.Groups))])
		}
	}
	for _, dev := range d.Devices {
		d.Owners[dev.ID()] = append(d.Owners[dev.ID()], principal())
		d.MemberOf[dev.ID()] = append(d.MemberOf[dev.ID()], d.Groups[r.Intn(len(d.Groups))])
	}

	for _, app := range d.Applications {
		if ownerless() {
			continue
		}
		d.Owners[app.ID()] = append(d.Owners[app.ID()], principal())
	}
	for i, sp := range d.ServicePrincipals {
		id := sp.ID()
		d.Owners[id] = append(d.Owners[id], principal())
		for range 1 + r.Intn(6) {
			d.Assignments[id] = append(d.Assignments[id], graph.Item{
				"id":                   fmt.Sprintf("ar-%s-%d", id, r.Intn(1000)),
				"principalId":          principal().ID(),
				"principalDisplayName": principal().String("displayName"),
				"principalType":        "User",
				"appRoleId":            fmt.Sprintf("role-%d", i),
			})
		}

		// Consent: roughly two thirds of what each app asks for is granted,
		// so the permissions tab has both states to show.
		for _, p := range graphPermissions {
			if r.Intn(3) == 0 {
				continue
			}
			if p.application {
				d.GrantedRoles[id] = append(d.GrantedRoles[id], p.id)
				continue
			}
			d.GrantedScopes[id] = append(d.GrantedScopes[id], p.value)
		}
	}
}

// ----------------------------------------------------------------- helpers

func personName(r *rand.Rand, i int) (display, login string) {
	if i%9 == 3 {
		// Hebrew names get a Latin sign-in name, as they usually do.
		return pick(r, hebrewGiven) + " " + pick(r, hebrewSurname), fmt.Sprintf("he.user%d", i)
	}
	given, family := givenNames[i%len(givenNames)], surnames[(i/len(givenNames))%len(surnames)]
	return fmt.Sprintf("%s %s", given, family), fmt.Sprintf("%s.%s%d", lower(given), lower(family), i)
}

func shortOS(name string) string {
	if len(name) < 3 {
		return name
	}
	return upper(name[:3])
}

func pick[T any](r *rand.Rand, options []T) T { return options[r.Intn(len(options))] }

func someTimeWithin(r *rand.Rand, days int) string {
	return time.Now().AddDate(0, 0, -r.Intn(max(days, 1))).UTC().Format(time.RFC3339)
}

func someTimeAhead(r *rand.Rand, days int) string {
	return time.Now().AddDate(0, 0, r.Intn(max(days, 1))).UTC().Format(time.RFC3339)
}

func lower(s string) string { return mapASCII(s, 'A', 'Z', 32) }
func upper(s string) string { return mapASCII(s, 'a', 'z', -32) }

func mapASCII(s string, lo, hi byte, delta int) string {
	out := []byte(s)
	for i, c := range out {
		if c >= lo && c <= hi {
			out[i] = byte(int(c) + delta)
		}
	}
	return string(out)
}

package auth

// Scope is an authorization scope string carried by an API key.
type Scope string

const (
	ScopeOrgAdmin     Scope = "org:admin"
	ScopePodAdmin     Scope = "pod:admin"
	ScopeInboxRead    Scope = "inbox:read"
	ScopeInboxWrite   Scope = "inbox:write"
	ScopeWebhookRead  Scope = "webhook:read"
	ScopeWebhookWrite Scope = "webhook:write"
	ScopeDraftRead    Scope = "draft:read"
	ScopeDraftWrite   Scope = "draft:write"
	ScopeSearchRead   Scope = "search:read"
)

// AllScopes contains every defined scope.
var AllScopes = []Scope{
	ScopeOrgAdmin, ScopePodAdmin,
	ScopeInboxRead, ScopeInboxWrite,
	ScopeWebhookRead, ScopeWebhookWrite,
	ScopeDraftRead, ScopeDraftWrite,
	ScopeSearchRead,
}

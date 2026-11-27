package app

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Every answer the MCP specification lets a client cache (the lists of
// tools, prompts, resources and resource templates, a resource read, the
// server's discovery) says who may keep it in cacheScope, which takes only
// "public" or "private". The SDK fills it on the answers it makes itself;
// the ones Leafen makes for the apps' prompts and resources (and the
// resource list it joins with them) went out with "cacheScope":"", and Claude
// Code refused each on every connect — "Invalid option: expected one of
// public|private" — and showed none of Leafen's resources or prompts
// (2026-11-23). What Leafen lists is the connection's own: its key's scope,
// its person's apps and services, in their language. So every such answer
// says private, decided here as it leaves the server, whoever made it.
func mcpAnswersCachedPrivately() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, request)
			if cacheable := mcpAnswerCacheable(result); err == nil && cacheable != nil {
				cacheable.CacheScope = "private"
			}
			return result, err
		}
	}
}

// mcpAnswerCacheable is the part of an answer that says how it may be
// cached; nil for an answer that has none.
func mcpAnswerCacheable(result mcp.Result) *mcp.Cacheable {
	switch typed := result.(type) {
	case *mcp.ListToolsResult:
		if typed != nil {
			return &typed.Cacheable
		}
	case *mcp.ListPromptsResult:
		if typed != nil {
			return &typed.Cacheable
		}
	case *mcp.ListResourcesResult:
		if typed != nil {
			return &typed.Cacheable
		}
	case *mcp.ListResourceTemplatesResult:
		if typed != nil {
			return &typed.Cacheable
		}
	case *mcp.ReadResourceResult:
		if typed != nil {
			return &typed.Cacheable
		}
	case *mcp.DiscoverResult:
		if typed != nil {
			return &typed.Cacheable
		}
	}
	return nil
}

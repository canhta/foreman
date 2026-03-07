// Package agentconst defines shared constants for the agent and agent/tools packages.
// It must not import either package to avoid import cycles.
package agentconst

// MaxAgentDepth is the maximum allowed nesting depth for subagent calls.
// A top-level agent is depth 0; its subagents are depth 1; etc.
const MaxAgentDepth = 3

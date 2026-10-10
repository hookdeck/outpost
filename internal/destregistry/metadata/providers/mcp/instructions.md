{{- /*
Instructions for the mcp destination type, shown in place of a create form
(create_mode "external"). This file is a Go text/template: the API renders it
once at startup with destmcp.RenderInstructions and serves the result as
markdown. Template comments like this one are not part of the output.

Data:
  .ServerURL  MCP_SERVER_URL, or "" when it isn't set.
  .Topics     The MCP-enabled topic names, in TOPICS order. May be empty.

Every value arrives as a markdown inline code span, backticks included, so
it can't break the surrounding markup: don't wrap it in backticks again. The
output must stay valid markdown when .ServerURL or .Topics is empty.
*/ -}}
# MCP Events

AI agents subscribe to events through {{if .ServerURL}}the MCP server at {{.ServerURL}}{{else}}our MCP server{{end}}, not from a form. Each subscription delivers the events the agent asked for to its MCP client, such as ChatGPT.

## Subscribe an agent

1. Connect {{if .ServerURL}}{{.ServerURL}}{{else}}our MCP server{{end}} in ChatGPT or another MCP client that supports MCP Events.
2. Ask your agent to subscribe to an event{{if .Topics}}, for example {{index .Topics 0}}{{end}}.
{{- if .Topics}}

Events agents can subscribe to:
{{range .Topics}}
- {{.}}
{{- end}}
{{- else}}

No events are available to agents yet.
{{- end}}

## Manage subscriptions

Each subscription shows up as a destination once an agent subscribes. Agents own their subscriptions, so they can't be edited here: disconnect one to stop its deliveries.

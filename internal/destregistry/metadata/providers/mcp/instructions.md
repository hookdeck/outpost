# MCP Events Configuration Instructions

Experimental. An MCP Events subscription delivers one topic's events to an HTTPS callback URL, as signed JSON messages in the MCP Events envelope.

Before the subscription is created, Outpost sends a verification challenge to the callback URL. The URL must answer with a 2xx JSON object that repeats the challenge in its `challenge` member.

Each delivery is a `POST` signed with the subscription secret, following the Standard Webhooks scheme. The subscription ID is sent in the `X-MCP-Subscription-Id` header.

# Portal

The portal is a React SPA embedded in the API service. During development, it can be difficult to achieve good DX with this setup because it requires 2 fairly heavy steps:

1: build the JS bundle
2: restart (rebuild && rerun) the Go API server

Because of that, it may take up to a few second for every change on the front end.

To support live reload, we run the Vite dev server in a separate process. When visiting the portal URL, instead of serving the static build, the API service will instead proxy to that Vite dev server.

We use the env variable `PORTAL_PROXY_URL` for this purpose. If this env is missing or is blank, it will serve the embedded built portal. We can consider making this env name more explicit that it's a dev-only value such as `DEV_PORTAL_PROXY_URL`, for example.

## Portal URL

Right now, the spec designs the portal to be a part of the API server. This could be in conflict with the idea that Outpost should be a private service, so it may or may not be exposed to the public.

We can consider supporting a dedicated Portal service or maybe distributing the built portal bundle so users can deploy it separately (either as a service or potentially as a static SPA). In that case, Outpost API will require a `PORTAL_URL` value to generate the portal redirect URL.

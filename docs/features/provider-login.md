# Provider login

The Kit server calls model providers, so it holds their credentials, in
`$KIT_HOME/auth.json` of the server's Kit home. Clients log the server in
through the session API. The server runs each login and stores the
credential; no client writes credentials or reads the credential file. This
works the same for a server on this machine, another Kit home, or a server
attached by URL.

## Providers and methods

| Provider | ID | Methods |
| --- | --- | --- |
| OpenAI Codex (ChatGPT plan) | `openai-codex` | device code |
| Anthropic | `anthropic` | Claude Pro or Max in a browser, or an API key |
| OpenAI | `openai` | API key |
| OpenCode Zen | `opencode` | API key |
| OpenCode Go | `opencode-go` | API key |

Environment variables in the server's environment take precedence over saved
credentials: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` or `ANTHROPIC_OAUTH_TOKEN`,
`OPENCODE_API_KEY` (for both OpenCode providers), and the `OPENAI_CODEX_*` variables. The server reads them
when it starts, and logins, API keys, and logout for such a provider are
refused, since they would have no effect.

## In the terminal client

When the server has no credentials for the model in use, the terminal client
offers the providers' logins. Each runs on the attached server:

- **Device code.** The client shows a URL and a code to enter there, from any
  device, and waits while the server polls the provider.
- **Browser.** The client shows the authorization page's URL. After approval,
  the browser redirects to `http://localhost:53692/callback` on the machine
  running it. The client listens there, on both loopback addresses, and relays
  a redirect carrying the login's state to the server, which exchanges it for
  a credential. The PKCE verifier needed for that exchange never leaves the
  server, so the URL and the redirect are useless to anyone else. If the
  client cannot listen on that port, it says so; then, or when the browser
  runs on another machine, paste the final page's URL or the code it shows.
- **API key.** The key is sent to the server once and never returned.

## From the command line

```sh
kit auth status                  # providers and whether the server is logged in
kit auth login openai-codex      # device-code login
kit auth logout PROVIDER         # remove the server's saved credential
```

`kit auth login` and `logout` act on this home's server, starting it as `kit`
does. `kit auth status` reports on the running server and never starts one.
To log in a server attached by URL, use the terminal client while attached to
it.

## Logins

A login is held by the server in memory. It ends when it succeeds, fails,
expires after 15 minutes, or is cancelled, and a finished login stays readable,
without its instructions, for 5 minutes. Starting a login to a provider with
one pending replaces the pending one, and logging out of a provider cancels
its pending login. A browser login takes one authorization. A client that
abandons its login, such as a terminal client whose login dialog is closed,
or that cannot deliver the authorization, cancels it; otherwise it expires.
Stopping the server fails its pending logins.

## Session API

| Operation | Purpose |
| --- | --- |
| `GET /v1/providers` | providers, their methods, and whether the server has a credential, from its store or environment |
| `POST /v1/logins` | start a device or browser login; returns its instructions |
| `GET /v1/logins/{loginID}?waitSeconds=N` | the login, after waiting up to 25 seconds for it to finish |
| `POST /v1/logins/{loginID}/authorization` | complete a browser login with the relayed redirect or a pasted code |
| `DELETE /v1/logins/{loginID}` | cancel a login |
| `PUT /v1/providers/{providerID}/api-key` | save an API key |
| `DELETE /v1/providers/{providerID}/credentials` | log out |

Go clients use `Client.Providers`, `StartProviderLogin`,
`WaitProviderLogin`, `SubmitProviderLoginAuthorization`, `CancelProviderLogin`,
`SetProviderAPIKey`, and `Logout`.

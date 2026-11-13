# Changelog

## [Unreleased]

## [2.5.3] - 2026-11-13

- fix(go-sdk): format cancellation test before release
- merge: integrate Windows WIP onto origin/main (CMS binding, SDK preview, dashboard, plugins, specs)

## [2.5.2] - 2026-11-13

- fix(tests,sdk): planner tests follow the A13-01 reserve; Go SDK passes gofmt
- fix(sdk): Go refunds do not follow redirects; Python timeout semantics documented

## [2.5.1] - 2026-11-13

- fix(plugins,publicweb): SDK webhook credit recipes, CMS recovery, redirects and CSP logs (audit 2026-11-13)
- docs(sdk): backfill published mirror changelogs across SDKs and MCP

## [2.5.0] - 2026-11-02

- feat(refunds): finalize clean merchant refunds spec, SDKs, CMS plugins, and targeted test scripts

- **403 is no longer reported as an authentication failure.** `(*APIError).Kind()` returns `"permission"` for 403; 401 keeps `"authentication"`. Same type, different category — code that switches on the category string must be updated.
- **Base URL must use HTTPS.** Plain HTTP is accepted only against a loopback host, which is what a local integration test points at. The API secret signs every request, so a plaintext base URL put that credential on the wire.

## [2.4.2] - 2026-11-02

- wip: snapshot shared working tree before merging main
- fix(merge): keep both sides for 126 conflicting files (union / semantic JSON merge); Balance.razor keeps ClickToCopy ID column
- wip(refunds): merchant refund plugin work (CMS adapters, PHP SDK, tests) - unverified WIP snapshot

- **403 is no longer reported as an authentication failure.** `(*APIError).Kind()` returns `"permission"` for 403; 401 keeps `"authentication"`. Same type, different category — code that switches on the category string must be updated.
- **Base URL must use HTTPS.** Plain HTTP is accepted only against a loopback host, which is what a local integration test points at. The API secret signs every request, so a plaintext base URL put that credential on the wire.

## [2.4.1] - 2026-11-01

- refactor(invoices): remove the multiple-payment toggle

- **403 is no longer reported as an authentication failure.** `(*APIError).Kind()` returns `"permission"` for 403; 401 keeps `"authentication"`. Same type, different category — code that switches on the category string must be updated.
- **Base URL must use HTTPS.** Plain HTTP is accepted only against a loopback host, which is what a local integration test points at. The API secret signs every request, so a plaintext base URL put that credential on the wire.

## [2.4.0] - 2026-10-29

- fix(sdk): preserve current refund contracts in TON conformance
- fix(ton): return friendly addresses with existing webhook schema
- feat(sdk-go): add refund create, get and list

- **403 is no longer reported as an authentication failure.** `(*APIError).Kind()` returns `"permission"` for 403; 401 keeps `"authentication"`. Same type, different category — code that switches on the category string must be updated.
- **Base URL must use HTTPS.** Plain HTTP is accepted only against a loopback host, which is what a local integration test points at. The API secret signs every request, so a plaintext base URL put that credential on the wire.

## [2.3.0] - 2026-10-14

- Verify invoice-late-payment audit fixes on the shared node
- Fix invoice-late-payment audit findings (tasks 4.1-4.8)
- feat(invoices): accept full late payments without reopening invoices

- **403 is no longer reported as an authentication failure.** `(*APIError).Kind()` returns `"permission"` for 403; 401 keeps `"authentication"`. Same type, different category — code that switches on the category string must be updated.
- **Base URL must use HTTPS.** Plain HTTP is accepted only against a loopback host, which is what a local integration test points at. The API secret signs every request, so a plaintext base URL put that credential on the wire.

## [2.2.1] - 2026-09-13

- fix(sdk-conformance): восемь копий контракта разошлись с каноном — 403 снова permission
- fix(sdk): HTTPS обязателен, а 403 перестал притворяться ошибкой ключа

- **403 is no longer reported as an authentication failure.** `(*APIError).Kind()` returns `"permission"` for 403; 401 keeps `"authentication"`. Same type, different category — code that switches on the category string must be updated.
- **Base URL must use HTTPS.** Plain HTTP is accepted only against a loopback host, which is what a local integration test points at. The API secret signs every request, so a plaintext base URL put that credential on the wire.

## [2.2.0] - 2026-08-28

- release: the changelog rot had a cause, and it was not the one I named
- docs(sdk): the READMEs say static wallets, not payment channels
- feat(sdk): payment-channel webhooks and the flow in every README
- feat(go-sdk): payment channels, and a feed reader that cannot stop early
- feat(sdk-conformance): declare the payment-channel operations
- docs(api): pin the empty-body signing rule and fix the published POST vector

## [2.1.1] - 2026-08-02

- fix(ecosystem): recover SDK releases

## [2.1.0] - 2026-07-21

- feat(docs): make the developer surface consumable by LLM agents

## [2.0.1] - 2026-07-19

- fix: align SDK problem details semantics

## [2.0.0]

- Move the module to the semantic import path `github.com/Paymos-labs/go-sdk/v2`.
- Replace untyped maps with typed requests, responses, pages, iterators, balances, and webhook events.

## [1.0.0]

- Initial official SDK.

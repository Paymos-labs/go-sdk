# Changelog

## [Unreleased]

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

# Changelog

## [1.10.0](https://github.com/matt-riley/flagz/compare/v1.9.2...v1.10.0) (2026-02-25)


### Features

* admin action audit logging ([#44](https://github.com/matt-riley/flagz/issues/44)) ([4caa06d](https://github.com/matt-riley/flagz/commit/4caa06d989b634dcc6208c482b85bad4f7ed833b))
* Admin Portal & Multi-Tenancy Support ([#25](https://github.com/matt-riley/flagz/issues/25)) ([35bcf62](https://github.com/matt-riley/flagz/commit/35bcf6271af0bf96856fd2a46c14abe7ca366d41))
* admin portal API key UI, audit log, and RBAC ([#48](https://github.com/matt-riley/flagz/issues/48)) ([a87f17e](https://github.com/matt-riley/flagz/commit/a87f17ef9b68823409e27dffc90d01a06242ccf7))
* API key ID context propagation ([#34](https://github.com/matt-riley/flagz/issues/34)) ([bb75d6e](https://github.com/matt-riley/flagz/commit/bb75d6e674c0c2301b205670ed0b7b53bd3b6a98))
* API key management endpoints ([#43](https://github.com/matt-riley/flagz/issues/43)) ([ffe7e42](https://github.com/matt-riley/flagz/commit/ffe7e42f4826cf7f54babbadbe269c53006f5a9c))
* audit logging for flag mutations ([#42](https://github.com/matt-riley/flagz/issues/42)) ([b17f96e](https://github.com/matt-riley/flagz/commit/b17f96e6cbfc008e2a9312ff34a45a456de7c8e8))
* auth failure rate limiting ([#39](https://github.com/matt-riley/flagz/issues/39)) ([3f2f8c2](https://github.com/matt-riley/flagz/commit/3f2f8c23530ba7a262661425503d8b207baa8012))
* configurable limits ([#37](https://github.com/matt-riley/flagz/issues/37)) ([b832f5a](https://github.com/matt-riley/flagz/commit/b832f5a27ec261e7ce38e0af807453e0b729848d))
* database connection pool metrics ([#40](https://github.com/matt-riley/flagz/issues/40)) ([7db0956](https://github.com/matt-riley/flagz/commit/7db095680445bb0fe6c76569864a0b79e83223db))
* go and typescript clients ([#14](https://github.com/matt-riley/flagz/issues/14)) ([8749353](https://github.com/matt-riley/flagz/commit/8749353c2f3ac9ced301a88f4bd1c539403709a8))
* initial commit ([849055c](https://github.com/matt-riley/flagz/commit/849055c654606ed98ebb66d42d5f8e687e9dc6b4))
* OpenTelemetry instrumentation for repository and service ([#45](https://github.com/matt-riley/flagz/issues/45)) ([28f39a4](https://github.com/matt-riley/flagz/commit/28f39a401bf04dfd6ba2db2c99591a9e54037072))
* OpenTelemetry tracing ([#36](https://github.com/matt-riley/flagz/issues/36)) ([062e8cb](https://github.com/matt-riley/flagz/commit/062e8cbd6b0b40d1c79b779e00900c3073e6c4bf))
* Phase 1 — Structured logging with slog ([#31](https://github.com/matt-riley/flagz/issues/31)) ([eb80c0d](https://github.com/matt-riley/flagz/commit/eb80c0d1427c357f76b6c9eaa98c206c122ec2d6))
* Phase 2 — Prometheus metrics with client_golang ([#33](https://github.com/matt-riley/flagz/issues/33)) ([086e5a5](https://github.com/matt-riley/flagz/commit/086e5a54213e71c43b5823abb24c4c602d57e037))
* SSE per-key filtering and HTTP pagination ([#35](https://github.com/matt-riley/flagz/issues/35)) ([a607638](https://github.com/matt-riley/flagz/commit/a6076388247d81886cb5b100c091d0a37de09d18))


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1 ([#21](https://github.com/matt-riley/flagz/issues/21)) ([4d48455](https://github.com/matt-riley/flagz/commit/4d48455230e70db74f1fba5f09f68dd14c051f70))
* **deps:** update module github.com/matt-riley/flagz to v1.1.1 ([#24](https://github.com/matt-riley/flagz/issues/24)) ([2522589](https://github.com/matt-riley/flagz/commit/252258986c5b6d4226220226f59650c669855c9d))
* **deps:** update module github.com/matt-riley/flagz to v1.2.0 ([#32](https://github.com/matt-riley/flagz/issues/32)) ([ae9201b](https://github.com/matt-riley/flagz/commit/ae9201bba2ccf20bd365d865124bca831dbd6ead))
* **deps:** update module github.com/matt-riley/flagz to v1.3.0 ([#52](https://github.com/matt-riley/flagz/issues/52)) ([6417d43](https://github.com/matt-riley/flagz/commit/6417d43fa51ca2873a88d10f68154a66a09517c1))
* **deps:** update module github.com/matt-riley/flagz to v1.3.1 ([#55](https://github.com/matt-riley/flagz/issues/55)) ([32653e5](https://github.com/matt-riley/flagz/commit/32653e547014da0e62e9daea49d9d4d219aee252))
* **deps:** update module github.com/matt-riley/flagz to v1.5.0 ([#62](https://github.com/matt-riley/flagz/issues/62)) ([77d9d8c](https://github.com/matt-riley/flagz/commit/77d9d8c69eb380c02e7983c1f6f43e9dfce633a5))
* **deps:** update module github.com/matt-riley/flagz to v1.7.0 ([#65](https://github.com/matt-riley/flagz/issues/65)) ([159f40d](https://github.com/matt-riley/flagz/commit/159f40d243a0bdfa5c6864e3cd083ef35f88620f))
* **deps:** update module github.com/matt-riley/flagz to v1.8.0 ([#68](https://github.com/matt-riley/flagz/issues/68)) ([5fa4eb3](https://github.com/matt-riley/flagz/commit/5fa4eb32552b5cf61bb5cf9108f39054ecfb6047))
* **deps:** update module github.com/matt-riley/flagz to v1.9.1 ([#70](https://github.com/matt-riley/flagz/issues/70)) ([38a749b](https://github.com/matt-riley/flagz/commit/38a749bcb839cff8911e260bc08517221cf2e722))
* **deps:** update module go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp to v1.40.0 ([#58](https://github.com/matt-riley/flagz/issues/58)) ([e70d684](https://github.com/matt-riley/flagz/commit/e70d684df522d4f38ec542e061904776b0edb5d4))
* **deps:** update module golang.org/x/time to v0.14.0 ([#59](https://github.com/matt-riley/flagz/issues/59)) ([f35f289](https://github.com/matt-riley/flagz/commit/f35f2892da111e72a798513e1d691f5ee0b79e45))
* make release guard repo-independent ([6d6fcfa](https://github.com/matt-riley/flagz/commit/6d6fcfab56c34ca8e1deae8b5f96e964911edfca))
* release-please ([66005d1](https://github.com/matt-riley/flagz/commit/66005d10fbee039b04176824048f800e199fa246))
* restore valid release and docker workflows ([ab924f2](https://github.com/matt-riley/flagz/commit/ab924f2479f619481960410a48ee3549f669bdcb))
* spelling mistake ([c880ea7](https://github.com/matt-riley/flagz/commit/c880ea75903e3391649339f29c4dc76d684e7310))

## [1.9.2](https://github.com/matt-riley/flagz/compare/v1.9.1...v1.9.2) (2026-02-25)


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.9.1 ([#70](https://github.com/matt-riley/flagz/issues/70)) ([38a749b](https://github.com/matt-riley/flagz/commit/38a749bcb839cff8911e260bc08517221cf2e722))

## [1.9.1](https://github.com/matt-riley/flagz/compare/v1.9.0...v1.9.1) (2026-02-24)


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.8.0 ([#68](https://github.com/matt-riley/flagz/issues/68)) ([5fa4eb3](https://github.com/matt-riley/flagz/commit/5fa4eb32552b5cf61bb5cf9108f39054ecfb6047))

## [1.9.0](https://github.com/matt-riley/flagz/compare/v1.8.0...v1.9.0) (2026-02-24)


### Features

* OpenTelemetry instrumentation for repository and service ([#45](https://github.com/matt-riley/flagz/issues/45)) ([28f39a4](https://github.com/matt-riley/flagz/commit/28f39a401bf04dfd6ba2db2c99591a9e54037072))


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.7.0 ([#65](https://github.com/matt-riley/flagz/issues/65)) ([159f40d](https://github.com/matt-riley/flagz/commit/159f40d243a0bdfa5c6864e3cd083ef35f88620f))
* make release guard repo-independent ([6d6fcfa](https://github.com/matt-riley/flagz/commit/6d6fcfab56c34ca8e1deae8b5f96e964911edfca))

## [1.8.0](https://github.com/matt-riley/flagz/compare/v1.7.0...v1.8.0) (2026-02-24)


### Features

* admin action audit logging ([#44](https://github.com/matt-riley/flagz/issues/44)) ([4caa06d](https://github.com/matt-riley/flagz/commit/4caa06d989b634dcc6208c482b85bad4f7ed833b))

## [1.7.0](https://github.com/matt-riley/flagz/compare/v1.6.0...v1.7.0) (2026-02-24)


### Features

* API key management endpoints ([#43](https://github.com/matt-riley/flagz/issues/43)) ([ffe7e42](https://github.com/matt-riley/flagz/commit/ffe7e42f4826cf7f54babbadbe269c53006f5a9c))


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.5.0 ([#62](https://github.com/matt-riley/flagz/issues/62)) ([77d9d8c](https://github.com/matt-riley/flagz/commit/77d9d8c69eb380c02e7983c1f6f43e9dfce633a5))

## [1.6.0](https://github.com/matt-riley/flagz/compare/v1.5.0...v1.6.0) (2026-02-23)


### Features

* database connection pool metrics ([#40](https://github.com/matt-riley/flagz/issues/40)) ([7db0956](https://github.com/matt-riley/flagz/commit/7db095680445bb0fe6c76569864a0b79e83223db))


### Bug Fixes

* **deps:** update module golang.org/x/time to v0.14.0 ([#59](https://github.com/matt-riley/flagz/issues/59)) ([f35f289](https://github.com/matt-riley/flagz/commit/f35f2892da111e72a798513e1d691f5ee0b79e45))

## [1.5.0](https://github.com/matt-riley/flagz/compare/v1.4.0...v1.5.0) (2026-02-23)


### Features

* auth failure rate limiting ([#39](https://github.com/matt-riley/flagz/issues/39)) ([3f2f8c2](https://github.com/matt-riley/flagz/commit/3f2f8c23530ba7a262661425503d8b207baa8012))
* configurable limits ([#37](https://github.com/matt-riley/flagz/issues/37)) ([b832f5a](https://github.com/matt-riley/flagz/commit/b832f5a27ec261e7ce38e0af807453e0b729848d))
* OpenTelemetry tracing ([#36](https://github.com/matt-riley/flagz/issues/36)) ([062e8cb](https://github.com/matt-riley/flagz/commit/062e8cbd6b0b40d1c79b779e00900c3073e6c4bf))
* SSE per-key filtering and HTTP pagination ([#35](https://github.com/matt-riley/flagz/issues/35)) ([a607638](https://github.com/matt-riley/flagz/commit/a6076388247d81886cb5b100c091d0a37de09d18))


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.3.1 ([#55](https://github.com/matt-riley/flagz/issues/55)) ([32653e5](https://github.com/matt-riley/flagz/commit/32653e547014da0e62e9daea49d9d4d219aee252))
* **deps:** update module go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp to v1.40.0 ([#58](https://github.com/matt-riley/flagz/issues/58)) ([e70d684](https://github.com/matt-riley/flagz/commit/e70d684df522d4f38ec542e061904776b0edb5d4))

## [1.4.0](https://github.com/matt-riley/flagz/compare/v1.3.1...v1.4.0) (2026-02-22)


### Features

* API key ID context propagation ([#34](https://github.com/matt-riley/flagz/issues/34)) ([bb75d6e](https://github.com/matt-riley/flagz/commit/bb75d6e674c0c2301b205670ed0b7b53bd3b6a98))

## [1.3.1](https://github.com/matt-riley/flagz/compare/v1.3.0...v1.3.1) (2026-02-22)


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.3.0 ([#52](https://github.com/matt-riley/flagz/issues/52)) ([6417d43](https://github.com/matt-riley/flagz/commit/6417d43fa51ca2873a88d10f68154a66a09517c1))

## [1.3.0](https://github.com/matt-riley/flagz/compare/v1.2.0...v1.3.0) (2026-02-22)


### Features

* Phase 1 — Structured logging with slog ([#31](https://github.com/matt-riley/flagz/issues/31)) ([eb80c0d](https://github.com/matt-riley/flagz/commit/eb80c0d1427c357f76b6c9eaa98c206c122ec2d6))
* Phase 2 — Prometheus metrics with client_golang ([#33](https://github.com/matt-riley/flagz/issues/33)) ([086e5a5](https://github.com/matt-riley/flagz/commit/086e5a54213e71c43b5823abb24c4c602d57e037))


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.2.0 ([#32](https://github.com/matt-riley/flagz/issues/32)) ([ae9201b](https://github.com/matt-riley/flagz/commit/ae9201bba2ccf20bd365d865124bca831dbd6ead))

## [1.2.0](https://github.com/matt-riley/flagz/compare/v1.1.1...v1.2.0) (2026-02-22)


### Features

* Admin Portal & Multi-Tenancy Support ([#25](https://github.com/matt-riley/flagz/issues/25)) ([35bcf62](https://github.com/matt-riley/flagz/commit/35bcf6271af0bf96856fd2a46c14abe7ca366d41))


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1.1.1 ([#24](https://github.com/matt-riley/flagz/issues/24)) ([2522589](https://github.com/matt-riley/flagz/commit/252258986c5b6d4226220226f59650c669855c9d))

## [1.1.1](https://github.com/matt-riley/flagz/compare/v1.1.0...v1.1.1) (2026-02-21)


### Bug Fixes

* **deps:** update module github.com/matt-riley/flagz to v1 ([#21](https://github.com/matt-riley/flagz/issues/21)) ([4d48455](https://github.com/matt-riley/flagz/commit/4d48455230e70db74f1fba5f09f68dd14c051f70))

## [1.1.0](https://github.com/matt-riley/flagz/compare/v1.0.0...v1.1.0) (2026-02-21)


### Features

* go and typescript clients ([#14](https://github.com/matt-riley/flagz/issues/14)) ([8749353](https://github.com/matt-riley/flagz/commit/8749353c2f3ac9ced301a88f4bd1c539403709a8))


### Bug Fixes

* release-please ([66005d1](https://github.com/matt-riley/flagz/commit/66005d10fbee039b04176824048f800e199fa246))
* spelling mistake ([c880ea7](https://github.com/matt-riley/flagz/commit/c880ea75903e3391649339f29c4dc76d684e7310))

## 1.0.0 (2026-02-20)


### Features

* initial commit ([849055c](https://github.com/matt-riley/flagz/commit/849055c654606ed98ebb66d42d5f8e687e9dc6b4))

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.1.0] - 2026-06-29

### Added

- Azure Service Bus Publisher and Subscriber for [gopipe](https://github.com/fxsml/gopipe)
- `PublisherProperties` and `SubscriberProperties` for broker-specific AMQP property mapping
- `EnablePeekMode` on `SubscriberConfig` for non-destructive message reading
- CLI tool (`gopipe-azservicebus`) with `pub` and `sub` commands (JSONL-based)

### Fixed

- Subscriber now processes Phase 1 synchronously to preserve message ordering
- CLI flag naming: `-T`/`--topic` for publish, `-t`/`--timeout` for subscribe

[v0.1.0]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.1.0

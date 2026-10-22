# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.3.0] - 2026-10-22

### Added

- `SubscriberConfig.SubQueue` to subscribe to a queue/subscription's dead-letter or transfer dead-letter sub-queue; `DeadLetterReason`/`DeadLetterErrorDescription` mapping on `SubscriberProperties`; `--dlq`/`--transfer-dlq` flags on the `sub` CLI command

## [v0.2.0] - 2026-08-11

### Added

- `pub` CLI command: `--format` flag to publish a single JSON (CloudEvents) event file in addition to JSONL, with auto-detection based on `.json` extension

### Changed

- **breaking:** bumped `gopipe/message` and `gopipe/pipe` to v0.19.0 (mirrors upstream's `Message` struct simplification) — `message.RawMessage` is renamed to `message.Message` across the public API (`Subscriber.Subscribe`, `Publisher.Publish`/`PublishBatch`, `SubscriberProperties`/`PublisherProperties` field types); `Publisher` now returns an error if a message reaching the broker boundary doesn't hold raw `[]byte` data

## [v0.1.0] - 2026-06-29

### Added

- Azure Service Bus Publisher and Subscriber for [gopipe](https://github.com/fxsml/gopipe)
- `PublisherProperties` and `SubscriberProperties` for broker-specific AMQP property mapping
- `EnablePeekMode` on `SubscriberConfig` for non-destructive message reading
- CLI tool (`gopipe-azservicebus`) with `pub` and `sub` commands (JSONL-based)

### Fixed

- Subscriber now processes Phase 1 synchronously to preserve message ordering
- CLI flag naming: `-T`/`--topic` for publish, `-t`/`--timeout` for subscribe

[v0.3.0]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.3.0
[v0.2.0]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.2.0
[v0.1.0]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.1.0

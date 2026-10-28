# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.4.0] - 2026-10-28

### Added

- `EnqueuedTime`, `DeliveryCount` and `SequenceNumber` mapping functions on `SubscriberProperties`
- `sub --broker-props` flag (opt-in) that adds `sbdeadletterreason`, `sbdeadlettererrordescription`, `sbenqueuedtime`, `sbdeliverycount` (number) and `sbsequencenumber` (string, since int64 sequence numbers can exceed 2^53 and would lose precision as a JSON number) extension attributes to the output

## [v0.3.1] - 2026-10-22

### Fixed

- `sub` CLI command: `--limit` flag was parsed but never enforced, so it read messages until `--timeout` instead of stopping after the configured count
- CLI no longer exits silently on command failure; errors are now logged before exit, and `pub`'s input-file-open error is wrapped with context

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

[v0.3.1]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.3.1
[v0.3.0]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.3.0
[v0.2.0]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.2.0
[v0.1.0]: https://github.com/fxsml/gopipe-azservicebus/releases/tag/v0.1.0

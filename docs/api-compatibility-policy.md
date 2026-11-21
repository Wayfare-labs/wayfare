# API Compatibility Policy

## Overview

Wayfare provides a versioned HTTP API under `/api/v1/...`. This document defines the compatibility guarantees, additive versus breaking changes, deprecation window, and header contract for consumers.

## Versioning Scheme

All stable API endpoints are prefixed with `/api/v1`. Unversioned routes (such as `/api/corridor` and `/api/assets`) are maintained as deprecated aliases to ease transition.

## Compatibility Guarantees

### Additive Changes (Non-Breaking)
- Adding new optional query parameters.
- Adding new fields to JSON response objects.
- Adding new endpoints.

These changes do not require a major version increment.

### Breaking Changes
- Removing or renaming existing fields or endpoints.
- Changing data types of existing fields (e.g., turning a decimal string into a JSON number).
- Changing semantic meaning of existing parameters or response fields.

Breaking changes will be introduced under a new major version prefix (e.g., `/api/v2`).

## Deprecation Window & Headers

When an endpoint or feature is deprecated, unversioned routes or deprecated fields will continue to be supported for a minimum deprecation window of 90 days.

Deprecated routes return the following HTTP response headers:
- `Deprecation: true`
- `Sunset: <RFC 7231 / HTTP-date representing expiration>`
- `Link: </api/v1/...>; rel="successor-version"`

## Client Responsibilities

Consumers should migrate to `/api/v1/...` endpoints before the expiration of the deprecation window.
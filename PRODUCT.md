# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Go backend. React, TypeScript, and Vite dashboard, explicitly selected by the user.

## Users

A University of Pittsburgh computer science student builds and explains the project. Internship interviewers and engineers inspect its code, run the local demonstration, and assess its failure handling and measurements.

## Product Purpose

Portunus is a portfolio LLM gateway. It routes application requests through providers while demonstrating retries, fallback, circuit recovery, streaming, caching, client traffic policies, and observable outcomes.

## Operating Context

One local command starts a gateway, two fake HTTP providers, and the dashboard. The operator sends requests and deliberately changes fake-provider behavior to inspect the gateway's response. No paid API calls are required for demonstrations or verification.

## Capabilities and Constraints

The full build includes configured provider adapters, exact caching, per-client rate and spending controls, request and attempt records, reproducible load tests, and a design-focused README. Single-instance in-memory state is a documented implementation choice. Accounts, billing, marketing pages, and customer acquisition are out of scope.

## Brand Commitments

The name is Portunus. The user requested a dark, minimal, nice-looking dashboard implemented in TypeScript. Product language should help explain real gateway behavior.

## Evidence on Hand

Stage-one source and HTTP/process tests are committed. New performance claims require actual benchmark output. Fake-provider usage and configured costs must be identified as synthetic or estimated. The dashboard must never display invented traffic as real observations.

## Product Principles

- Favor depth of failure handling over additional product features.
- Keep the implementation understandable enough to defend in interviews.
- Make every demonstration repeatable without provider credentials.
- Distinguish a completed request from its individual provider attempts.
- Report measured evidence with its workload and limits.

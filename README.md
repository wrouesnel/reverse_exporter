[![Build and Test](https://github.com/wrouesnel/reverse_exporter/actions/workflows/integration.yml/badge.svg)](https://github.com/wrouesnel/reverse_exporter/actions/workflows/integration.yml)
[![Release](https://github.com/wrouesnel/reverse_exporter/actions/workflows/release.yml/badge.svg)](https://github.com/wrouesnel/reverse_exporter/actions/workflows/release.yml)
[![Container Build](https://github.com/wrouesnel/reverse_exporter/actions/workflows/container.yml/badge.svg)](https://github.com/wrouesnel/reverse_exporter/actions/workflows/container.yml)
[![Coverage Status](https://coveralls.io/repos/github/wrouesnel/reverse_exporter/badge.svg?branch=main)](https://coveralls.io/github/wrouesnel/reverse_exporter?branch=main)
r)

# Reverse Prometheus Exporter

Logical-decoding Promethus Metric Reverse Proxy

# Getting Started

A prebuilt docker container is hosted on Github Packages:

```bash
docker run -it -p 9998:9998 -v /reverse_exporter.yml:/config/reverse_exporter.yml ghcr.io/wrouesnel/reverse_exporter
```

Or you can build your own:
```bash
docker build -t reverse_exporter .
docker run -p 9998:9998 -v /myconfig.yml:/config/reverse_exporter.yml
```

## Purpose

This exporter is designed for appliance-like container environments where multiple Prometheus
exporters should be presented as a single "instance" to a Prometheus server.

The reverse_exporter logically decodes its target exporters on each scrape, allowing them to be 
presented as unique metrics to Prometheus. It appends a new field (enforced to be unique) of `exporter_name`
to each metric so name-colliding metrics from internal exporters can be differeniated (i.e. since most Prometheus
exporters export their own process information as a part of their metrics).

tl;dr It's how you get `/metrics` to work with a fat container.

# Notable Functionality

* Combine and merge multiple exporters into a single `/metrics` endpoint
* Append and override metric labels on all reverse proxied metrics
* Support exposing metrics from static files on disk
* Support intelligent on-scrape dynamic metrics from scripts 
  (multiple scrapes are queued to single script execution preventing overloading)
* Support periodic (cron-like) dynamic metrics from scripts
* TLS support.
* Authentication support via HTTP basic auth and/or TLS client-certificates.

## Quick Start

Run `docker-compose up` in the root of the repository to build and start a
`reverse_exporter` combining the metrics of a Prometheus instance with a
`node_exporter`.

Browse to http://127.0.0.1:9998/metrics to view the results.

## Usage

See `example.config.yml` for a config file including all parameters used in
some way.

# Hacking

To get started with the repository run `go run mage.go autogen` to configure
your repositories build hooks.

To build a binary for your current platform run `go run mage.go binary`

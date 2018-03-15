[![Build Status](https://travis-ci.org/wrouesnel/reverse_exporter.svg?branch=master)](https://travis-ci.org/wrouesnel/reverse_exporter)
[![Coverage Status](https://coveralls.io/repos/github/wrouesnel/reverse_exporter/badge.svg?branch=master)](https://coveralls.io/github/wrouesnel/reverse_exporter?branch=master)
[![Go Report Card](https://goreportcard.com/badge/github.com/wrouesnel/reverse_exporter)](https://goreportcard.com/report/github.com/wrouesnel/reverse_exporter)

# Reverse Prometheus Exporter

Logical-decoding Promethus Metric Reverse Proxy

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

## Quick Start

Run `docker-compose up` in the root of the repository to build and start a
`reverse_exporter` combining the metrics of a Prometheus instance with a
`node_exporter`.

Browse to [http://127.0.0.1:9998/metrics] to view the results.

## Usage

See `example.config.yml` for a config file including all parameters used in
some way.

## Building

Build system is based on Mage. Simply run `go run mage.go` to invoke
the magefile.


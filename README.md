
# APRS Web Telnet Gateway

A lightweight web-based APRS-IS client built in Go.  
The tool allows logging into an APRS-IS server via Telnet from a browser, sending APRS messages without needing UI-based desktop software.  
Designed for persistent sessions, low resource usage and easy deployment (native or Docker).

[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)]()
[![License](https://img.shields.io/badge/License-MIT-green.svg)]()
[![Status](https://img.shields.io/badge/Project-Active-success.svg)]()
[![Docker Ready](https://img.shields.io/badge/Docker-Ready-informational.svg)]()

## Features

- Bootstrap 5 web UI
- Login with Callsign + APRS passcode
- Telnet persistent session (no re-login)
- APRS message sending via APRS-IS
- Automatic padding and character validation
- Filters APRS login banner after first appearance
- WebSocket communication
- Extensible and container friendly
- Logging of transmissions (with IP, callsign, target and message)

## Requirements

- Go 1.21+ or Docker
- APRS-IS reachable server (default 14580)
- Callsign + APRS passcode

## Configuration

Environment variables:

TELNET_SERVER=rotate.aprs2.net\
TELNET_PORT=14580\
APRS_VERSION="APRSClient 1.0"\

## Running

### Using Docker

```bash
docker build -t aprs-web .\
docker run -p 8080:8080   -e TELNET_SERVER=rotate.aprs2.net   -e TELNET_PORT=14580   -e APRS_VERSION="APRSClient 1.0"   aprs-web
```

## docker-compose example

```yaml
version: "3"

services:
  aprs-web:
    image: aprs-web:latest
    container_name: aprs-web
    ports:
      - "8080:8080"
    environment:
      - TELNET_SERVER=rotate.aprs2.net
      - TELNET_PORT=14580
      - APRS_VERSION=APRSWeb 1.0
```

## APRS Message Format

EA5ABC>APRS,TCPIP*,qAC,T2SERVER::EA5XYZ :Hello from gateway

Destination is padded automatically to 9 chars.

## Logs

Each send request logs:

timestamp | client_ip | callsign | destination | message

## License

MIT

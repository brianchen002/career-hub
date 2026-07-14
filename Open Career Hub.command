#!/bin/bash

# Double-click launcher for Career Hub on macOS.
set -e

cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

if [ ! -x "./career-hub" ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "Go is required to build Career Hub. Install Go 1.24+ from https://go.dev/dl/"
    read -n 1 -s -r -p "Press any key to close…"
    exit 1
  fi
  go build -o career-hub .
fi

exec ./career-hub
